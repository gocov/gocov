#!/usr/bin/env bash
# deploy-branch.sh — roll app.gocov.dev to the working tree, from a laptop.
#
# The release path (deploy.yml) deploys a *released* image: a v-tag, on
# GHCR, for both architectures, with a CLI binary on the GitHub release
# for the smoke upload. Trying a branch before it is merged needs none of
# that, so this script does the same rollout by hand:
#
#   1. builds the server image for the Fargate task's architecture
#      (linux/arm64) from the checked-out tree and pushes it to a private
#      ECR repository the script creates on first use (gocov-server-dev,
#      images expire after 14 days — the release images stay on GHCR);
#   2. registers a task-definition revision that differs from the
#      template only in the image, exactly as deploy.yml does, and rolls
#      the service to it;
#   3. waits for the rollout, checks the running task is on that image,
#      and hits /healthz.
#
# It runs as the local AWS identity, not the workflow's OIDC role, and it
# skips the production environment's approval click and the real-upload
# smoke: this is the "try it on the live instance" path, and what it
# deploys is whatever is on disk — uncommitted edits included (the tag
# says -dirty when so). Migrations are forward-only, so a branch that
# adds one leaves its schema behind when rolled back.
#
# Usage:
#   scripts/deploy-branch.sh                 build the tree and roll to it
#   scripts/deploy-branch.sh --image REF     roll to an existing image
#                                            (ghcr.io/gocov/gocov-server:v<release>
#                                            is the rollback to that release)
#
# Needs: aws (signed in to the gocov account), docker with buildx, jq.
set -euo pipefail

export AWS_REGION=eu-central-1
ACCOUNT=773658094601
CLUSTER=gocov
SERVICE=gocov-server
LOG_GROUP=/gocov/server
PUBLIC_URL=https://app.gocov.dev
ECR_REPO=gocov-server-dev
ECR_HOST="$ACCOUNT.dkr.ecr.$AWS_REGION.amazonaws.com"

root=$(git rev-parse --show-toplevel)
cd "$root"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

image=""
case "${1:-}" in
  --image) image=${2:?--image needs an image reference} ;;
  "") ;;
  *) echo "usage: $0 [--image REF]" >&2; exit 2 ;;
esac

if [ -z "$image" ]; then
  # A tag that says what is running: branch, commit, and whether the tree
  # had uncommitted changes when the image was built.
  branch=$(git branch --show-current | tr -c 'A-Za-z0-9._\n' '-')
  sha=$(git rev-parse --short HEAD)
  dirty=""
  [ -z "$(git status --porcelain)" ] || dirty="-dirty"
  tag="${branch:-detached}-$sha$dirty"
  image="$ECR_HOST/$ECR_REPO:$tag"

  # First use: the repository, with a lifecycle policy so trial images do
  # not pile up. Idempotent afterwards.
  if ! aws ecr describe-repositories --repository-names "$ECR_REPO" >/dev/null 2>&1; then
    echo "creating ECR repository $ECR_REPO"
    aws ecr create-repository --repository-name "$ECR_REPO" \
      --image-scanning-configuration scanOnPush=false >/dev/null
    aws ecr put-lifecycle-policy --repository-name "$ECR_REPO" --lifecycle-policy-text '{
      "rules": [{"rulePriority": 1, "description": "trial images expire",
        "selection": {"tagStatus": "any", "countType": "sinceImagePushed",
                      "countUnit": "days", "countNumber": 14},
        "action": {"type": "expire"}}]}' >/dev/null
  fi
  aws ecr get-login-password | docker login --username AWS --password-stdin "$ECR_HOST" >/dev/null

  echo "building $image"
  docker buildx build --platform linux/arm64 --build-arg "VERSION=$tag" \
    -t "$image" --push .
fi

# The task definition: the template from the tree, the image swapped in,
# every /gocov/<NAME> parameter wired as the secret NAME — the same jq as
# deploy.yml, so what this registers is what a release would.
echo "registering a task definition for $image"
aws ssm describe-parameters --parameter-filters "Key=Path,Values=/gocov/" \
  --query 'Parameters[].Name' --output json >"$work/params.json"
jq -e 'all(.[]; test("^/gocov/[A-Z][A-Z0-9_]*$"))' "$work/params.json" >/dev/null || {
  echo "a /gocov/ parameter is not named like an environment variable:" >&2
  cat "$work/params.json" >&2; exit 1; }
jq --arg image "$image" \
   --arg prefix "arn:aws:ssm:$AWS_REGION:$ACCOUNT:parameter" \
   --slurpfile names "$work/params.json" '
  ($names[0] | map(ltrimstr("/gocov/"))) as $secret
  | .containerDefinitions[0].image = $image
  | .containerDefinitions[0].secrets =
      [ $names[0][] | {name: ltrimstr("/gocov/"), valueFrom: ($prefix + .)} ]
  | .containerDefinitions[0].environment |=
      map(select(.name as $n | $secret | index($n) | not))
' deploy/ecs/task-definition.json >"$work/taskdef.json"
taskdef=$(aws ecs register-task-definition --cli-input-json "file://$work/taskdef.json" \
  --query taskDefinition.taskDefinitionArn --output text)
echo "registered $taskdef"

echo "rolling $SERVICE"
aws ecs update-service --cluster "$CLUSTER" --service "$SERVICE" \
  --task-definition "$taskdef" --query service.serviceArn --output text >/dev/null

# Same wait as deploy.yml: the stable waiter, then the PRIMARY
# deployment's own verdict, since a circuit-breaker rollback also ends
# "stable" and the state flips to COMPLETED a moment after the waiter.
aws ecs wait services-stable --cluster "$CLUSTER" --services "$SERVICE" || true
primary() {
  aws ecs describe-services --cluster "$CLUSTER" --services "$SERVICE" \
    --query 'services[0].deployments[?status==`PRIMARY`] | [0].{taskDefinition: taskDefinition, state: rolloutState, reason: rolloutStateReason, running: runningCount}' \
    --output json
}
for _ in $(seq 1 30); do
  state=$(primary | jq -r .state)
  if [ "$state" = COMPLETED ] || [ "$state" = FAILED ]; then
    break
  fi
  sleep 10
done
primary
if [ "$(primary | jq -r .taskDefinition)" != "$taskdef" ] || [ "$(primary | jq -r .state)" != COMPLETED ]; then
  echo "rollout of $taskdef did not complete — the service is on $(primary | jq -r .taskDefinition)" >&2
  echo "--- last 10 minutes of $LOG_GROUP" >&2
  aws logs tail "$LOG_GROUP" --since 10m --format short >&2 || true
  exit 1
fi

task=$(aws ecs list-tasks --cluster "$CLUSTER" --service-name "$SERVICE" \
  --desired-status RUNNING --query 'taskArns[0]' --output text)
aws ecs describe-tasks --cluster "$CLUSTER" --tasks "$task" \
  --query 'tasks[0].containers[0].image' --output text | grep -Fx "$image" >/dev/null
curl -fsS --retry 5 --retry-delay 3 --retry-all-errors "$PUBLIC_URL/healthz" >/dev/null

echo
echo "$PUBLIC_URL is on $image"
echo "back to the release: $0 --image ghcr.io/gocov/gocov-server:$(git describe --tags --abbrev=0 origin/main 2>/dev/null || echo vX.Y.Z)"
