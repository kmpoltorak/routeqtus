"""Temporarily break observable guarantees, require test failure, always restore."""
import os
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parent.parent
os.chdir(root)
if not os.environ.get("ROUTETUS_TEST_DATABASE_URL"):
    raise SystemExit("ROUTETUS_TEST_DATABASE_URL is required for publication sabotage")

cases = [
    ("internal/strictjson/json.go", "if seen[key] {", "if false {",
     ["go", "test", "./internal/strictjson", "-run", "TestAmbiguousDocuments", "-count=1"], "TestAmbiguousDocuments"),
    ("internal/gateway/gateway.go", "middleware.LimitBody(writer, r, s.limits.MaxBodyBytes)", "middleware.LimitBody(writer, r, 1<<40)",
     ["go", "test", "./internal/gateway", "-run", "TestTimeoutCancellationAndBodyLimits", "-count=1"], "TestTimeoutCancellationAndBodyLimits"),
    ("internal/gateway/gateway.go", "if s.trustedIP(peer) {", "if true {",
     ["go", "test", "./internal/gateway", "-run", "TestForwarding", "-count=1"], "TestForwarding"),
    ("internal/postgres/store.go", "if generation != expected {", "if false {",
     ["go", "test", "-tags=integration", "./tests/integration", "-run", "TestControlPlaneAndReload", "-count=1", "-timeout=60s"], "TestControlPlaneAndReload"),
    ("internal/app/app.go", "config.SetLimits(c.Limits)", "config.SetLimits(config.DefaultLimits)",
     ["go", "test", "./internal/app", "-run", "TestBootstrapLimitsReachCompilerAndSchema", "-count=1"], "TestBootstrapLimitsReachCompilerAndSchema"),
    ("internal/postgres/store.go", "if v == 1 {", "if false {",
     ["go", "test", "-tags=integration", "./tests/integration", "-run", "TestMigrationUpgradesVersionOneSchema", "-count=1", "-timeout=60s"], "TestMigrationUpgradesVersionOneSchema"),
    ("internal/extensions/concurrency.go", "active.Add(1) > int64(o.Limit)", "active.Add(1) > 1000000000",
     ["go", "test", "./internal/gateway", "-run", "^TestHandlerBindingConcurrencyScope$", "-count=1"], "TestHandlerBindingConcurrencyScope"),
    ("internal/extensions/rate.go", "allowed := tokens >= 1", "allowed := true",
     ["go", "test", "./internal/extensions", "-run", "^TestRateLimitBurstRefillAndParallelAdmission$", "-count=1"], "TestRateLimitBurstRefillAndParallelAdmission"),
    ("internal/extensions/tokens.go", "if matched != 1 {", "if false {",
     ["go", "test", "./internal/extensions", "-run", "^TestTokenAllowlist$", "-count=1"], "TestTokenAllowlist"),
    ("internal/extensions/tokens.go", 'r.Header.Del("Authorization")', 'r.Header.Del("X-Unused")',
     ["go", "test", "./internal/gateway", "-run", "^TestRouteTokensNeverReachBackendOrLogs$", "-count=1"], "TestRouteTokensNeverReachBackendOrLogs"),
    ("internal/middleware/middleware.go", 'logger.Info("request", "request_id"', 'logger.Info("request", "authorization", r.Header.Get("Authorization"), "request_id"',
     ["go", "test", "./internal/gateway", "-run", "^TestRouteTokensNeverReachBackendOrLogs$", "-count=1"], "TestRouteTokensNeverReachBackendOrLogs"),
    ("internal/extensions/body.go", "middleware.LimitBody(w, r, o.MaxBytes)", "middleware.LimitBody(w, r, 1<<30)",
     ["go", "test", "./internal/gateway", "-run", "^TestRouteBodyLimitPreservesProcessCeiling$", "-count=1"], "TestRouteBodyLimitPreservesProcessCeiling"),
    ("internal/bootstrap/config.go", "c.Server.MaxBodyBytes > 1<<30", "c.Server.MaxBodyBytes > 1<<40",
     ["go", "test", "./internal/bootstrap", "-run", "^TestServerBodyLimitMaximumIsOneGiB$", "-count=1"], "TestServerBodyLimitMaximumIsOneGiB"),
    ("internal/app/app.go", "config.SetLimits(c.Limits)", "config.SetLimits(config.DefaultLimits)",
     ["go", "test", "./internal/app", "-run", "^TestBootstrapPolicyLimitsReachCompilerAndSchema$", "-count=1"], "TestBootstrapPolicyLimitsReachCompilerAndSchema"),
    ("internal/gateway/gateway.go", 'r.ServiceID != "" && !config.ValidID(r.ServiceID)', "false",
     ["go", "test", "./internal/gateway", "-run", "^TestOptionalServiceIDsAreAlphanumeric$", "-count=1"], "TestOptionalServiceIDsAreAlphanumeric"),
    ("internal/postgres/store.go", "if v == 2 {", "if false {",
     ["go", "test", "-tags=integration", "./tests/integration", "-run", "^TestMigrationFromVersionTwoPreservesRevision$", "-count=1", "-timeout=60s"], "TestMigrationFromVersionTwoPreservesRevision"),

]
for filename, needle, replacement, command, test in cases:
    path = root / filename
    original = path.read_text()
    if original.count(needle) != 1:
        raise SystemExit(f"Expected exactly one sabotage location in {filename}: {needle}")
    try:
        path.write_text(original.replace(needle, replacement, 1))
        result = subprocess.run(command, text=True, capture_output=True, timeout=90)
        output = result.stdout + result.stderr
        if result.returncode == 0 or f"--- FAIL: {test}" not in output:
            raise SystemExit(f"Sabotage was not caught by the intended test:\n{output}")
        print(f"sabotage check: removed {needle} -> {test} failed")
        for line in output.splitlines():
            if "--- FAIL:" in line or ".go:" in line:
                print(line)
    finally:
        path.write_text(original)
        assert path.read_text() == original
print("All original source files restored. Run the complete checks after sabotage.")
