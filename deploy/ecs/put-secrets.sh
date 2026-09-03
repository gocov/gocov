#!/bin/sh
# Copies the hosted instance's configuration into SSM Parameter Store,
# one SecureString per variable under /gocov/, which is where the Fargate
# task reads it from (deploy.yml turns every /gocov/<NAME> parameter into
# the container's NAME). Run it wherever the .env lives — on the EC2 box
# over an SSM session, with a temporary ssm:PutParameter grant on its
# instance role — so the values never pass through a laptop or a CI log.
#
#   deploy/ecs/put-secrets.sh [.env] [github-app.pem]
#
# Re-running overwrites; that is how a rotated secret reaches the task
# (followed by a deploy, which registers a new task revision that reads
# the new value at start). Keys the task definition sets as plain
# environment, and GOCOV_VERSION, are skipped: the image tag is the
# deploy's business, not a parameter.
set -eu

env_file=${1:-/opt/gocov/deploy/.env}
pem_file=${2:-/opt/gocov/deploy/github-app.pem}
region=${AWS_REGION:-eu-central-1}

put() {
	aws ssm put-parameter --region "$region" --name "/gocov/$1" \
		--type SecureString --value "$2" --overwrite >/dev/null
	echo "/gocov/$1"
}

while IFS= read -r line || [ -n "$line" ]; do
	case $line in ''|'#'*) continue ;; esac
	key=${line%%=*}
	val=${line#*=}
	# Strip one pair of matching quotes, the way compose reads .env.
	case $val in
	\"*\") val=${val#\"}; val=${val%\"} ;;
	\'*\') val=${val#\'}; val=${val%\'} ;;
	esac
	case $key in
	GOCOV_VERSION|GOCOV_ADDR|GOCOV_BASE_URL|GOCOV_MODE|GOCOV_GITHUB_APP_PRIVATE_KEY) continue ;;
	esac
	[ -n "$val" ] || continue
	put "$key" "$val"
done <"$env_file"

# The App private key is a file on the box; in the task it is the PEM
# itself in the variable, which the server accepts directly.
if [ -f "$pem_file" ]; then
	put GOCOV_GITHUB_APP_PRIVATE_KEY "$(cat "$pem_file")"
fi
