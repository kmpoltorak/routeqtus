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
    ("internal/gateway/gateway.go", "http.MaxBytesReader(writer, r.Body, s.limits.MaxBodyBytes)", "http.MaxBytesReader(writer, r.Body, 1<<40)",
     ["go", "test", "./internal/gateway", "-run", "TestTimeoutCancellationAndBodyLimits", "-count=1"], "TestTimeoutCancellationAndBodyLimits"),
    ("internal/gateway/gateway.go", "if s.trustedIP(peer) {", "if true {",
     ["go", "test", "./internal/gateway", "-run", "TestForwarding", "-count=1"], "TestForwarding"),
    ("internal/postgres/store.go", "if generation != expected {", "if false {",
     ["go", "test", "-tags=integration", "./tests/integration", "-run", "TestControlPlaneAndReload", "-count=1", "-timeout=60s"], "TestControlPlaneAndReload"),
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
