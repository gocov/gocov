# Hosted deployment (app.gocov.dev)

One Fargate task running `ghcr.io/gocov/gocov-server` behind an Application
Load Balancer, in front of an RDS Postgres. Cloudflare proxies the hostname
on Full (strict); the ALB terminates TLS with an ACM certificate. Nothing
is built or configured on a machine: the image comes from GHCR by exact
version, configuration comes from SSM Parameter Store, logs go to
CloudWatch, and a deploy is a task-definition revision that differs from
the last one only in the image tag. (Until September 2026 this was one
EC2 instance running [`docker-compose.prod.yml`](docker-compose.prod.yml)
plus Caddy; that file stays as the self-host starting point and is smoked
on every release.)

## What runs where (eu-central-1)

| Piece | Name | Notes |
|---|---|---|
| ECS cluster / service | `gocov` / `gocov-server` | 1 task, Fargate, ARM64 (Graviton), 0.5 vCPU / 1 GB. Rolling deploys with the circuit breaker on: a revision whose task never turns healthy is rolled back by ECS itself. |
| Task definition | family `gocov-server` | Registered by `deploy.yml` from [`ecs/task-definition.json`](ecs/task-definition.json); the workflow fills in the image and the secrets. |
| Load balancer | ALB `gocov`, target group `gocov-server` | HTTPS 443 only, `ELBSecurityPolicy-TLS13-1-2-2021-06`, idle timeout 120 s for large uploads; target health is `GET /healthz`. |
| Certificate | ACM, `app.gocov.dev` + `origin.gocov.dev` | DNS-validated, auto-renews. |
| Configuration | SSM Parameter Store `/gocov/*` | One SecureString per variable, [below](#configuration). |
| Logs | CloudWatch `/gocov/server` | 30-day retention. |
| Alarms | SNS `gocov-alarms` → email | Task CPU/memory, ALB 5xx, unhealthy targets. |
| Database | RDS `gocov-db` | db.t4g.micro, Postgres 18, 20 GB gp3, encrypted, 7-day backups, not public. Unchanged by the migration. |
| Network | default VPC, the three public subnets | Tasks get a public IP for outbound (GHCR, the forges) — no NAT gateway. |
| Security groups | `gocov-alb` 443 ← internet · `gocov-ecs` 8080 ← `gocov-alb` · `gocov-db` 5432 ← `gocov-ecs` | |
| IAM | `gocov-ecs-execution` (task execution: logs, `/gocov/*` parameters) · `gocov-deploy` (the workflow's OIDC role) | The task itself has no AWS role: the server calls no AWS API. |
| DNS (Cloudflare) | `app.gocov.dev` → CNAME to the ALB, proxied · `origin.gocov.dev` → CNAME to the ALB, DNS only | The second name is what the deploy smoke test and any direct check use; it bypasses Cloudflare the way the old instance's public IP always did. |

## Configuration

Every variable the server reads is a SecureString parameter named
`/gocov/<VARIABLE>`; at deploy time `deploy.yml` lists the names under
`/gocov/` and hands each to the container as that variable, resolved by
ECS through the execution role when the task starts. Values never pass
through the workflow. The three non-secret settings (`GOCOV_ADDR`,
`GOCOV_BASE_URL`, `GOCOV_MODE`) live as plain environment in the template;
a parameter with the same name would override one.

- **Add or rotate a value**: `aws ssm put-parameter --name /gocov/NAME --type SecureString --value ... --overwrite`, then deploy (or re-deploy the current tag): a running task keeps the values it started with.
- **Turn a forge off**: delete its parameters, then deploy. A missing parameter is not an empty variable — the template never mentions names, so removing the parameter removes the variable.
- **Everything at once** — from a compose-style `.env` and an App key file: [`ecs/put-secrets.sh`](ecs/put-secrets.sh), run wherever those files are (step 5 of the setup did this once, on the old instance).
- The GitHub App private key is the PEM itself in `GOCOV_GITHUB_APP_PRIVATE_KEY`; the server accepts either a path or the key, and a parameter cannot be a file.

## Deploys

Every release deploys itself: `release.yml` builds the multi-arch image,
pushes it to GHCR, then calls `deploy.yml`, which waits on the
`production` environment for approval. Approving is the whole human part
of a deploy. The workflow then assumes the `gocov-deploy` OIDC role,
registers a task definition for the tag, updates the service, waits for
the rollout, checks that the primary deployment is the new revision and
that the running container reports the released image, then smoke-tests:
`/healthz` through `origin.gocov.dev` and `app.gocov.dev`, plus a real
upload to `gocov/smoke` through the origin with the release's own CLI
binary. Migrations apply automatically on start, as always.

**Rolling means two tasks for a moment.** The new task must be healthy
before the old one is stopped, so for ~30 seconds both serve traffic and
share the database. Everything that must not run twice takes a Postgres
advisory lock — the per-commit merge and status push always did, and the
grant refresh (Bitbucket and GitLab rotate the refresh token on every
use, so two concurrent refreshes would drop the grant) moved onto one for
this migration. Deploys are zero-downtime as a result.

**Rollback**: dispatch `deploy.yml` by hand with any older release tag —
same job, no build, about a minute. A rollout that fails its health check
never needs this: ECS's circuit breaker puts the previous revision back
and the workflow reports the failure with the last ten minutes of logs.
Migrations are forward-only, so rolling the image back never rolls the
schema back; if the schema itself is suspect, that is RDS point-in-time
recovery, not a redeploy.

## Operating it

- **Logs**: `aws logs tail /gocov/server --follow` (or `--since 1h`).
- **What is running**: `aws ecs describe-services --cluster gocov --services gocov-server --query 'services[0].deployments'`.
- **There is no shell to get into.** The image is distroless — no `sh`, so ECS Exec cannot start a session in it, and there is nothing on a task's disk to look at: the container writes nothing. Logs are the whole story.
- **Database access**: there is no standing bastion. `gocov-db` accepts 5432 from `gocov-ecs` only, so the way in is a throwaway EC2 (or a one-off Fargate task) in that security group with `psql`, deleted afterwards.
- **Resize**: `cpu`/`memory` in the task template (then a deploy), `--desired-count` on the service. Horizontal scale is *possible* — the locks above are what made it so — but not needed at this load.
- **Cost** (approx., ARM Fargate): task ≈ $15/month, ALB ≈ $20 + two public IPv4 ≈ $7, task IPv4 ≈ $4, logs ≈ $1–3. About $25–30/month more than the instance was, bought against: no machine to patch, no disk or Docker log to fill, deploy and rollback in a minute, and a rolling deploy instead of a restart gap.

## One-time setup

Everything below was done once; it is here so it can be redone or read.
Region `eu-central-1`, account `773658094601`, default VPC. Shell
variables used throughout:

```sh
export AWS_REGION=eu-central-1
ACCOUNT=773658094601
VPC=vpc-071b2af82d821474d
SUBNETS=subnet-04d8167fc69c04518,subnet-0501b5d54ff086ec4,subnet-092f316c9d744e695
DB_SG=sg-0112b298a08335eff   # gocov-db
```

**1. Security groups.** The ALB is the only thing the internet reaches; the
tasks only hear from the ALB; the database only from the tasks.

```sh
ALB_SG=$(aws ec2 create-security-group --group-name gocov-alb --vpc-id $VPC \
  --description "gocov ALB: 443 from the internet" --query GroupId --output text)
aws ec2 authorize-security-group-ingress --group-id $ALB_SG --protocol tcp --port 443 --cidr 0.0.0.0/0
ECS_SG=$(aws ec2 create-security-group --group-name gocov-ecs --vpc-id $VPC \
  --description "gocov Fargate tasks: 8080 from the ALB" --query GroupId --output text)
aws ec2 authorize-security-group-ingress --group-id $ECS_SG --protocol tcp --port 8080 --source-group $ALB_SG
aws ec2 authorize-security-group-ingress --group-id $DB_SG --protocol tcp --port 5432 --source-group $ECS_SG
```

**2. Log group.**

```sh
aws logs create-log-group --log-group-name /gocov/server
aws logs put-retention-policy --log-group-name /gocov/server --retention-in-days 30
```

**3. Task execution role** — pulls (public image, so only logs matter from
the managed policy) and reads the `/gocov/*` parameters, which means
decrypting with the account's SSM key.

```sh
aws iam create-role --role-name gocov-ecs-execution --assume-role-policy-document '{
  "Version": "2012-10-17",
  "Statement": [{"Effect": "Allow", "Principal": {"Service": "ecs-tasks.amazonaws.com"}, "Action": "sts:AssumeRole"}]}'
aws iam attach-role-policy --role-name gocov-ecs-execution \
  --policy-arn arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy
SSM_KEY=$(aws kms describe-key --key-id alias/aws/ssm --query KeyMetadata.Arn --output text)
aws iam put-role-policy --role-name gocov-ecs-execution --policy-name gocov-parameters --policy-document "{
  \"Version\": \"2012-10-17\",
  \"Statement\": [
    {\"Effect\": \"Allow\", \"Action\": \"ssm:GetParameters\", \"Resource\": \"arn:aws:ssm:${AWS_REGION}:${ACCOUNT}:parameter/gocov/*\"},
    {\"Effect\": \"Allow\", \"Action\": \"kms:Decrypt\", \"Resource\": \"$SSM_KEY\"}]}"
```

**4. The deploy role learns ECS.** `gocov-deploy` (OIDC, trusted only for
`repo:gocov/gocov:environment:production`) may do exactly what a deploy
needs and nothing else:

```sh
aws iam put-role-policy --role-name gocov-deploy --policy-name gocov-deploy-ecs --policy-document "{
  \"Version\": \"2012-10-17\",
  \"Statement\": [
    {\"Sid\": \"TaskDefinitions\", \"Effect\": \"Allow\",
     \"Action\": [\"ecs:RegisterTaskDefinition\", \"ecs:DescribeTaskDefinition\"], \"Resource\": \"*\"},
    {\"Sid\": \"TheOneService\", \"Effect\": \"Allow\",
     \"Action\": [\"ecs:UpdateService\", \"ecs:DescribeServices\"],
     \"Resource\": \"arn:aws:ecs:${AWS_REGION}:${ACCOUNT}:service/gocov/gocov-server\"},
    {\"Sid\": \"SeeItsTasks\", \"Effect\": \"Allow\", \"Action\": [\"ecs:ListTasks\", \"ecs:DescribeTasks\"], \"Resource\": \"*\",
     \"Condition\": {\"ArnEquals\": {\"ecs:cluster\": \"arn:aws:ecs:${AWS_REGION}:${ACCOUNT}:cluster/gocov\"}}},
    {\"Sid\": \"HandTheTaskItsRole\", \"Effect\": \"Allow\", \"Action\": \"iam:PassRole\",
     \"Resource\": \"arn:aws:iam::${ACCOUNT}:role/gocov-ecs-execution\",
     \"Condition\": {\"StringEquals\": {\"iam:PassedToService\": \"ecs-tasks.amazonaws.com\"}}},
    {\"Sid\": \"ParameterNamesOnly\", \"Effect\": \"Allow\", \"Action\": \"ssm:DescribeParameters\", \"Resource\": \"*\"},
    {\"Sid\": \"ReadServerLogs\", \"Effect\": \"Allow\",
     \"Action\": [\"logs:DescribeLogGroups\", \"logs:DescribeLogStreams\", \"logs:GetLogEvents\", \"logs:FilterLogEvents\"], \"Resource\": \"*\"}]}"
```

**5. Configuration into Parameter Store.** The values lived only on the
old instance, so the copy ran there: its role got `ssm:PutParameter` on
`/gocov/*` for the duration, the script ran over an SSM session, the
grant came off again. Starting from scratch, the script runs wherever a
`.env` and the App key are written.

```sh
AWS_REGION=eu-central-1 deploy/ecs/put-secrets.sh path/to/.env path/to/github-app.pem
aws ssm describe-parameters --parameter-filters Key=Path,Values=/gocov/ --query 'Parameters[].Name'
```

**6. Certificate.** Request, then create the validation CNAMEs it asks for
in Cloudflare (DNS only — a proxied validation record does not validate).

```sh
CERT=$(aws acm request-certificate --domain-name app.gocov.dev \
  --subject-alternative-names origin.gocov.dev --validation-method DNS \
  --query CertificateArn --output text)
aws acm describe-certificate --certificate-arn $CERT \
  --query 'Certificate.DomainValidationOptions[].ResourceRecord'
aws acm wait certificate-validated --certificate-arn $CERT
```

**7. Load balancer.** Across all three subnets (an ALB needs at least two;
the tasks may land in any of them).

```sh
ALB=$(aws elbv2 create-load-balancer --name gocov --type application --scheme internet-facing \
  --subnets ${SUBNETS//,/ } --security-groups $ALB_SG \
  --query 'LoadBalancers[0].LoadBalancerArn' --output text)
aws elbv2 modify-load-balancer-attributes --load-balancer-arn $ALB --attributes \
  Key=idle_timeout.timeout_seconds,Value=120 \
  Key=routing.http.drop_invalid_header_fields.enabled,Value=true
TG=$(aws elbv2 create-target-group --name gocov-server --protocol HTTP --port 8080 --vpc-id $VPC \
  --target-type ip --health-check-path /healthz --health-check-interval-seconds 10 \
  --healthy-threshold-count 2 --unhealthy-threshold-count 3 \
  --query 'TargetGroups[0].TargetGroupArn' --output text)
aws elbv2 modify-target-group-attributes --target-group-arn $TG \
  --attributes Key=deregistration_delay.timeout_seconds,Value=15
aws elbv2 create-listener --load-balancer-arn $ALB --protocol HTTPS --port 443 \
  --certificates CertificateArn=$CERT --ssl-policy ELBSecurityPolicy-TLS13-1-2-2021-06 \
  --default-actions Type=forward,TargetGroupArn=$TG
aws elbv2 describe-load-balancers --load-balancer-arns $ALB --query 'LoadBalancers[0].DNSName' --output text
```

**8. Cluster, first task definition, service.** The first revision is
registered by hand with the same transformation the workflow applies
(image and secrets filled in); after that only `deploy.yml` registers
revisions.

```sh
aws ecs create-cluster --cluster-name gocov
TAG=v0.16.0   # the release in production at the time
aws ssm describe-parameters --parameter-filters Key=Path,Values=/gocov/ \
  --query 'Parameters[].Name' --output json >params.json
jq --arg image "ghcr.io/gocov/gocov-server:$TAG" \
   --arg prefix "arn:aws:ssm:${AWS_REGION}:${ACCOUNT}:parameter" --slurpfile names params.json '
  ($names[0] | map(ltrimstr("/gocov/"))) as $secret
  | .containerDefinitions[0].image = $image
  | .containerDefinitions[0].secrets = [ $names[0][] | {name: ltrimstr("/gocov/"), valueFrom: ($prefix + .)} ]
  | .containerDefinitions[0].environment |= map(select(.name as $n | $secret | index($n) | not))
' deploy/ecs/task-definition.json >taskdef.json
aws ecs register-task-definition --cli-input-json file://taskdef.json \
  --query taskDefinition.taskDefinitionArn --output text
aws ecs create-service --cluster gocov --service-name gocov-server \
  --task-definition gocov-server --desired-count 1 --launch-type FARGATE --platform-version LATEST \
  --network-configuration "awsvpcConfiguration={subnets=[$SUBNETS],securityGroups=[$ECS_SG],assignPublicIp=ENABLED}" \
  --load-balancers targetGroupArn=$TG,containerName=server,containerPort=8080 \
  --health-check-grace-period-seconds 60 \
  --deployment-configuration "minimumHealthyPercent=100,maximumPercent=200,deploymentCircuitBreaker={enable=true,rollback=true}"
aws ecs wait services-stable --cluster gocov --services gocov-server
aws elbv2 describe-target-health --target-group-arn $TG --query 'TargetHealthDescriptions[].TargetHealth.State'
```

**9. Alarms** — one topic, one email subscription (confirm the mail it
sends), four alarms that page on the things a single task can do wrong.

```sh
TOPIC=$(aws sns create-topic --name gocov-alarms --query TopicArn --output text)
aws sns subscribe --topic-arn $TOPIC --protocol email --notification-endpoint you@example.com
ALB_ID=${ALB#*:loadbalancer/}; TG_ID=${TG#*:}
aws cloudwatch put-metric-alarm --alarm-name gocov-task-cpu --namespace AWS/ECS --metric-name CPUUtilization \
  --dimensions Name=ClusterName,Value=gocov Name=ServiceName,Value=gocov-server \
  --statistic Average --period 300 --evaluation-periods 3 --threshold 80 --comparison-operator GreaterThanThreshold \
  --alarm-actions $TOPIC --ok-actions $TOPIC
aws cloudwatch put-metric-alarm --alarm-name gocov-task-memory --namespace AWS/ECS --metric-name MemoryUtilization \
  --dimensions Name=ClusterName,Value=gocov Name=ServiceName,Value=gocov-server \
  --statistic Average --period 300 --evaluation-periods 3 --threshold 80 --comparison-operator GreaterThanThreshold \
  --alarm-actions $TOPIC --ok-actions $TOPIC
aws cloudwatch put-metric-alarm --alarm-name gocov-alb-5xx --namespace AWS/ApplicationELB --metric-name HTTPCode_Target_5XX_Count \
  --dimensions Name=LoadBalancer,Value=$ALB_ID \
  --statistic Sum --period 300 --evaluation-periods 1 --threshold 5 --comparison-operator GreaterThanThreshold \
  --treat-missing-data notBreaching --alarm-actions $TOPIC --ok-actions $TOPIC
aws cloudwatch put-metric-alarm --alarm-name gocov-unhealthy-targets --namespace AWS/ApplicationELB --metric-name UnHealthyHostCount \
  --dimensions Name=LoadBalancer,Value=$ALB_ID Name=TargetGroup,Value=$TG_ID \
  --statistic Maximum --period 60 --evaluation-periods 3 --threshold 0 --comparison-operator GreaterThanThreshold \
  --alarm-actions $TOPIC --ok-actions $TOPIC
```

**10. DNS.** In Cloudflare: `origin.gocov.dev` → CNAME to the ALB's DNS
name, DNS only; `app.gocov.dev` → CNAME to the same name, proxied. (The
cutover itself was the second record: it had been an A record to the
instance's Elastic IP, and moving it behind the proxy was invisible to
users. The instance was then stopped, terminated, and its Elastic IP,
`gocov-web` security group, `gocov-ec2` role and the `gocov-deploy` SSM
policy removed.)

### GitHub side

- **GHCR package public**: the first push of `ghcr.io/gocov/gocov-server`
  creates a private package — make it public (package settings) and link
  it to the repo. Production pulls it with no credential.
- **Environment `production`** on gocov/gocov with a required reviewer.
  This is the deploy gate; without it every release would deploy
  unattended.
- **AWS OIDC**: an IAM OIDC provider for `token.actions.githubusercontent.com`
  (audience `sts.amazonaws.com`) and the role `gocov-deploy`, trusted only
  for `repo:gocov/gocov:environment:production`, with the inline policy
  from step 4.
- **Smoke repo `gocov/smoke`**: a public repo with a trivial Go module
  and one test, tracked in the gocov workspace on app.gocov.dev, so the
  workspace's `GOCOV_TOKEN` secret (already used by ci.yml) accepts its
  uploads.
