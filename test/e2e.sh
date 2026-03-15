#!/bin/bash
#
# End-to-end test suite for hangon.
# Run: make e2e
#
# Tests the full CLI by building the binary and exercising it against real
# backends (process, TCP). Each test is isolated with a temporary HOME.
#
# Exit code = number of failed tests (0 = all pass).
#
set -uo pipefail

# --- Configuration ---
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BINARY=""
TEST_HOME=""
PASS=0
FAIL=0
ERRORS=()

# --- Colors ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

# --- Setup / Teardown ---

setup() {
    echo -e "${BOLD}Building hangon...${NC}"
    BINARY="$PROJECT_DIR/hangon-e2e-$$"
    if ! (cd "$PROJECT_DIR" && go build -o "$BINARY" .); then
        echo -e "${RED}Build failed${NC}"
        exit 2
    fi

    TEST_HOME="$(mktemp -d)"
    export HOME="$TEST_HOME"
    echo "  Binary:    $BINARY"
    echo "  Test HOME: $TEST_HOME"
    echo ""
}

teardown() {
    "$BINARY" stopall 2>/dev/null || true
    sleep 0.5

    # Kill any orphaned tmux sessions from tests.
    tmux list-sessions 2>/dev/null | grep "^hangon-" | cut -d: -f1 | while read -r s; do
        tmux kill-session -t "$s" 2>/dev/null || true
    done

    rm -f "$BINARY"
    rm -rf "$TEST_HOME"
}

# --- Test helpers ---

hangon() {
    "$BINARY" "$@"
}

# Run a command, capture stdout+stderr and exit code.
capture() {
    local __output __code=0
    __output=$("$@" 2>&1) || __code=$?
    echo "$__output"
    return $__code
}

pass() {
    PASS=$((PASS + 1))
    echo -e "  ${GREEN}PASS${NC}  $1"
}

fail() {
    FAIL=$((FAIL + 1))
    ERRORS+=("$1")
    echo -e "  ${RED}FAIL${NC}  $1"
}

# Clean up all sessions before/after each test.
clean_sessions() {
    hangon stopall 2>/dev/null || true
    sleep 0.3
}

run_test() {
    local name="$1"
    echo -e "\n${YELLOW}--- $name ---${NC}"
    clean_sessions
    "$name"
    clean_sessions
}

# --- Tests: CLI basics ---

test_version() {
    local out
    out=$(capture hangon --version)
    if echo "$out" | grep -q "hangon"; then
        pass "hangon --version prints version"
    else
        fail "hangon --version: unexpected output: $out"
    fi
}

test_help() {
    local out
    out=$(capture hangon --help)
    if echo "$out" | grep -q "COMMANDS" && echo "$out" | grep -q "start"; then
        pass "hangon --help contains COMMANDS and start"
    else
        fail "hangon --help: missing expected content"
    fi
}

test_subcommand_help() {
    local failed=0
    for cmd in start stop send sendline read expect screen keys alive wait screenshot; do
        local out
        out=$(capture hangon "$cmd" --help) || true
        if [ -z "$out" ]; then
            fail "hangon $cmd --help: no output"
            failed=1
        fi
    done
    if [ "$failed" -eq 0 ]; then
        pass "all subcommands have --help"
    fi
}

test_unknown_command() {
    local code=0
    hangon foobar >/dev/null 2>&1 || code=$?
    if [ "$code" -eq 2 ]; then
        pass "unknown command exits 2"
    else
        fail "unknown command: expected exit 2, got $code"
    fi
}

# --- Tests: Process backend ---

test_process_start_stop() {
    hangon start process --name test-ss -- python3 -i 2>&1
    sleep 1

    local out
    out=$(capture hangon list)
    if echo "$out" | grep -q "test-ss"; then
        pass "start creates session visible in list"
    else
        fail "start/list: session not found in list: $out"
    fi

    hangon stop test-ss 2>&1
    sleep 0.5

    out=$(capture hangon list)
    if echo "$out" | grep -q "test-ss"; then
        fail "stop: session still in list after stop: $out"
    else
        pass "stop removes session from list"
    fi
}

test_process_sendline_expect() {
    hangon start process --name calc -- python3 -i 2>&1
    hangon expect calc ">>>" --timeout 10 >/dev/null 2>&1

    hangon sendline calc "2 + 2" 2>&1 >/dev/null
    local out
    out=$(capture hangon expect calc "4" --timeout 5) || true
    if echo "$out" | grep -q "4"; then
        pass "sendline + expect round-trip"
    else
        fail "sendline/expect: expected '4' in output, got: $out"
    fi

    hangon stop calc 2>/dev/null
}

test_process_read() {
    hangon start process --name reader -- python3 -i 2>&1
    hangon expect reader ">>>" --timeout 10 >/dev/null 2>&1

    hangon sendline reader "print('hello_world_read')" >/dev/null 2>&1
    hangon expect reader "hello_world_read" --timeout 5 >/dev/null 2>&1

    # After expect consumed the output, send something new and read it.
    hangon sendline reader "print('fresh_output')" >/dev/null 2>&1
    sleep 1
    local out
    out=$(capture hangon read reader)
    if echo "$out" | grep -q "fresh_output"; then
        pass "read returns new output"
    else
        fail "read: expected 'fresh_output', got: $out"
    fi

    hangon stop reader 2>/dev/null
}

test_process_readall() {
    hangon start process --name ra -- python3 -i 2>&1
    hangon expect ra ">>>" --timeout 10 >/dev/null 2>&1

    hangon sendline ra "print('line_one')" >/dev/null 2>&1
    hangon expect ra "line_one" --timeout 5 >/dev/null 2>&1

    hangon sendline ra "print('line_two')" >/dev/null 2>&1
    hangon expect ra "line_two" --timeout 5 >/dev/null 2>&1

    local out
    out=$(capture hangon readall ra)
    if echo "$out" | grep -q "line_one" && echo "$out" | grep -q "line_two"; then
        pass "readall returns complete buffer"
    else
        fail "readall: expected both 'line_one' and 'line_two', got: $out"
    fi

    hangon stop ra 2>/dev/null
}

test_process_screen() {
    hangon start process --name scr -- python3 -i 2>&1
    hangon expect scr ">>>" --timeout 10 >/dev/null 2>&1

    local out
    out=$(capture hangon screen scr)
    if echo "$out" | grep -q ">>>"; then
        pass "screen shows terminal content"
    else
        fail "screen: expected '>>>' in output, got: $out"
    fi

    hangon stop scr 2>/dev/null
}

test_process_alive() {
    hangon start process --name alive-t -- python3 -i 2>&1
    hangon expect alive-t ">>>" --timeout 10 >/dev/null 2>&1

    local code=0
    hangon alive alive-t >/dev/null 2>&1 || code=$?
    if [ "$code" -eq 0 ]; then
        pass "alive returns 0 when process is running"
    else
        fail "alive: expected exit 0, got $code"
    fi

    # Kill the process.
    hangon keys alive-t "ctrl-d" >/dev/null 2>&1
    sleep 2

    code=0
    hangon alive alive-t >/dev/null 2>&1 || code=$?
    if [ "$code" -eq 1 ]; then
        pass "alive returns 1 when process has exited"
    else
        fail "alive after exit: expected exit 1, got $code"
    fi

    hangon stop alive-t 2>/dev/null || true
}

test_process_keys() {
    hangon start process --name keys-t -- python3 -i 2>&1
    hangon expect keys-t ">>>" --timeout 10 >/dev/null 2>&1

    # Start an infinite loop and interrupt with ctrl-c.
    hangon sendline keys-t "while True: pass" >/dev/null 2>&1
    sleep 0.5
    hangon keys keys-t "ctrl-c" >/dev/null 2>&1

    local out
    out=$(capture hangon expect keys-t ">>>" --timeout 5) || true
    if echo "$out" | grep -q ">>>"; then
        pass "keys ctrl-c interrupts running code"
    else
        fail "keys: expected new prompt after ctrl-c, got: $out"
    fi

    hangon stop keys-t 2>/dev/null
}

test_process_status() {
    hangon start process --name stat -- python3 -i 2>&1
    hangon expect stat ">>>" --timeout 10 >/dev/null 2>&1

    local out
    out=$(capture hangon status stat)
    if echo "$out" | grep -q "process" && echo "$out" | grep -q "stat"; then
        pass "status shows type and session name"
    else
        fail "status: expected 'process' and 'stat', got: $out"
    fi

    hangon stop stat 2>/dev/null
}

test_process_screenshot() {
    hangon start process --name shot -- python3 -i 2>&1
    hangon expect shot ">>>" --timeout 10 >/dev/null 2>&1

    local screenshot_file="$TEST_HOME/test-shot.png"
    local out
    out=$(capture hangon screenshot shot "$screenshot_file")
    if echo "$out" | grep -qE '\.(svg|png)$'; then
        local actual_file
        actual_file=$(echo "$out" | tr -d '[:space:]')
        if [ -f "$actual_file" ]; then
            local size
            size=$(wc -c < "$actual_file")
            if [ "$size" -gt 100 ]; then
                pass "screenshot creates valid file ($size bytes)"
            else
                fail "screenshot: file too small ($size bytes)"
            fi
        else
            fail "screenshot: file not found: $actual_file"
        fi
    else
        fail "screenshot: expected file path in output, got: $out"
    fi

    hangon stop shot 2>/dev/null
}

test_expect_timeout() {
    hangon start process --name to -- python3 -i 2>&1
    hangon expect to ">>>" --timeout 10 >/dev/null 2>&1

    local code=0
    hangon expect to "THIS_WILL_NEVER_APPEAR" --timeout 2 >/dev/null 2>&1 || code=$?
    if [ "$code" -eq 1 ]; then
        pass "expect timeout returns exit 1"
    else
        fail "expect timeout: expected exit 1, got $code"
    fi

    hangon stop to 2>/dev/null
}

test_expect_sequential() {
    hangon start process --name seq -- python3 -i 2>&1
    hangon expect seq ">>>" --timeout 10 >/dev/null 2>&1

    hangon sendline seq "print('FIRST_SEQ')" >/dev/null 2>&1
    hangon expect seq "FIRST_SEQ" --timeout 5 >/dev/null 2>&1

    hangon sendline seq "print('SECOND_SEQ')" >/dev/null 2>&1
    local out
    out=$(capture hangon expect seq "SECOND_SEQ" --timeout 5) || true
    if echo "$out" | grep -q "SECOND_SEQ"; then
        pass "sequential expects work correctly"
    else
        fail "sequential expect: expected 'SECOND_SEQ', got: $out"
    fi

    hangon stop seq 2>/dev/null
}

test_expect_no_stale_match() {
    # Critical test: expect must not match patterns from already-consumed output.
    hangon start process --name stale -- python3 -i 2>&1
    hangon expect stale ">>>" --timeout 10 >/dev/null 2>&1

    hangon sendline stale "print('UNIQUE_XYZ_123')" >/dev/null 2>&1
    hangon expect stale "UNIQUE_XYZ_123" --timeout 5 >/dev/null 2>&1

    # Now expect a fresh version of the same marker. It has NOT been sent again,
    # so this should timeout. If expect rescans from buffer start, it would
    # incorrectly match the old output.
    local code=0
    hangon expect stale "UNIQUE_XYZ_123" --timeout 2 >/dev/null 2>&1 || code=$?
    if [ "$code" -eq 1 ]; then
        pass "expect does not match already-consumed output"
    else
        fail "expect stale data: expected timeout (exit 1), got exit $code"
    fi

    hangon stop stale 2>/dev/null
}

test_read_after_expect() {
    # After expect consumes output, read should only return NEW data.
    hangon start process --name rae -- python3 -i 2>&1
    hangon expect rae ">>>" --timeout 10 >/dev/null 2>&1

    hangon sendline rae "print('CONSUMED_BY_EXPECT')" >/dev/null 2>&1
    hangon expect rae "CONSUMED_BY_EXPECT" --timeout 5 >/dev/null 2>&1

    hangon sendline rae "print('AFTER_EXPECT')" >/dev/null 2>&1
    sleep 1
    local out
    out=$(capture hangon read rae)

    local ok=1
    if echo "$out" | grep -q "AFTER_EXPECT"; then
        : # good
    else
        fail "read after expect: expected 'AFTER_EXPECT', got: $out"
        ok=0
    fi

    # Should NOT contain the already-consumed marker.
    if echo "$out" | grep -q "CONSUMED_BY_EXPECT"; then
        fail "read after expect: should not contain already-consumed 'CONSUMED_BY_EXPECT'"
        ok=0
    fi

    if [ "$ok" -eq 1 ]; then
        pass "read after expect returns only new data"
    fi

    hangon stop rae 2>/dev/null
}

# --- Tests: Named sessions ---

test_named_sessions() {
    hangon start process --name alpha -- python3 -i 2>&1
    hangon start process --name beta -- python3 -i 2>&1

    hangon expect alpha ">>>" --timeout 10 >/dev/null 2>&1
    hangon expect beta ">>>" --timeout 10 >/dev/null 2>&1

    hangon sendline alpha "print('FROM_ALPHA')" >/dev/null 2>&1
    hangon sendline beta "print('FROM_BETA')" >/dev/null 2>&1

    hangon expect alpha "FROM_ALPHA" --timeout 5 >/dev/null 2>&1
    hangon expect beta "FROM_BETA" --timeout 5 >/dev/null 2>&1

    local out
    out=$(capture hangon list)
    if echo "$out" | grep -q "alpha" && echo "$out" | grep -q "beta"; then
        pass "multiple named sessions coexist"
    else
        fail "named sessions: list doesn't show both: $out"
    fi

    hangon stop alpha 2>/dev/null
    hangon stop beta 2>/dev/null
}

test_default_session() {
    hangon start process -- python3 -i 2>&1
    hangon expect ">>>" --timeout 10 >/dev/null 2>&1

    hangon sendline "print('default_test')" >/dev/null 2>&1
    local out
    out=$(capture hangon expect "default_test" --timeout 5) || true
    if echo "$out" | grep -q "default_test"; then
        pass "default session name works"
    else
        fail "default session: expected 'default_test', got: $out"
    fi

    hangon stop 2>/dev/null
}

test_duplicate_name() {
    hangon start process --name dup -- python3 -i 2>&1
    hangon expect dup ">>>" --timeout 10 >/dev/null 2>&1

    local code=0
    hangon start process --name dup -- python3 -i >/dev/null 2>&1 || code=$?
    if [ "$code" -eq 2 ]; then
        pass "duplicate session name is rejected"
    else
        fail "duplicate name: expected exit 2, got $code"
    fi

    hangon stop dup 2>/dev/null
}

test_stopall() {
    hangon start process --name s1 -- python3 -i 2>&1
    hangon start process --name s2 -- python3 -i 2>&1
    hangon expect s1 ">>>" --timeout 10 >/dev/null 2>&1
    hangon expect s2 ">>>" --timeout 10 >/dev/null 2>&1

    hangon stopall 2>&1 >/dev/null
    sleep 1

    local out
    out=$(capture hangon list)
    if echo "$out" | grep -q "No active"; then
        pass "stopall removes all sessions"
    else
        fail "stopall: sessions remain: $out"
    fi
}

# --- Tests: Error handling ---

test_nonexistent_session() {
    local failed=0
    for cmd in "read nosuch" "sendline nosuch hello" "stop nosuch" "status nosuch" "alive nosuch"; do
        local code=0
        eval hangon $cmd >/dev/null 2>&1 || code=$?
        if [ "$code" -ne 2 ]; then
            fail "nonexistent session ($cmd): expected exit 2, got $code"
            failed=1
        fi
    done
    if [ "$failed" -eq 0 ]; then
        pass "operations on nonexistent sessions exit 2"
    fi
}

test_bad_args() {
    local code=0
    hangon start >/dev/null 2>&1 || code=$?
    if [ "$code" -eq 2 ]; then
        pass "start without type exits 2"
    else
        fail "start no type: expected exit 2, got $code"
    fi
}

# --- Tests: TCP backend ---

test_tcp_backend() {
    # Find a free port.
    local port
    port=$(python3 -c "import socket; s=socket.socket(); s.bind(('',0)); print(s.getsockname()[1]); s.close()")

    # Start a simple TCP echo server.
    python3 -c "
import socket
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(('127.0.0.1', $port))
s.listen(1)
conn, addr = s.accept()
try:
    while True:
        data = conn.recv(1024)
        if not data:
            break
        conn.sendall(data)
except:
    pass
conn.close()
s.close()
" &
    local server_pid=$!
    sleep 0.5

    hangon start tcp --name tcpecho localhost:$port 2>&1
    sleep 0.5

    hangon send tcpecho "hello_tcp
" >/dev/null 2>&1
    local out
    out=$(capture hangon expect tcpecho "hello_tcp" --timeout 5) || true
    if echo "$out" | grep -q "hello_tcp"; then
        pass "TCP send/expect echo works"
    else
        fail "TCP: expected 'hello_tcp', got: $out"
    fi

    local code=0
    hangon alive tcpecho >/dev/null 2>&1 || code=$?
    if [ "$code" -eq 0 ]; then
        pass "TCP alive returns 0 when connected"
    else
        fail "TCP alive: expected exit 0, got $code"
    fi

    hangon stop tcpecho 2>/dev/null
    kill $server_pid 2>/dev/null || true
    wait $server_pid 2>/dev/null || true
}

test_tcp_read() {
    local port
    port=$(python3 -c "import socket; s=socket.socket(); s.bind(('',0)); print(s.getsockname()[1]); s.close()")

    # Server that sends a greeting then echoes.
    python3 -c "
import socket
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(('127.0.0.1', $port))
s.listen(1)
conn, addr = s.accept()
conn.sendall(b'WELCOME\n')
try:
    while True:
        data = conn.recv(1024)
        if not data:
            break
        conn.sendall(data)
except:
    pass
conn.close()
s.close()
" &
    local server_pid=$!
    sleep 0.5

    hangon start tcp --name tcprd localhost:$port 2>&1
    sleep 0.5

    # Read the greeting.
    local out
    out=$(capture hangon expect tcprd "WELCOME" --timeout 5) || true
    if echo "$out" | grep -q "WELCOME"; then
        pass "TCP read receives server greeting"
    else
        fail "TCP greeting: expected 'WELCOME', got: $out"
    fi

    hangon stop tcprd 2>/dev/null
    kill $server_pid 2>/dev/null || true
    wait $server_pid 2>/dev/null || true
}

# --- Tests: Process --no-pty ---

test_no_pty_stdout() {
    hangon start process --name nopty --no-pty -- python3 -c "
import sys
sys.stdout.write('stdout_marker\n')
sys.stdout.flush()
import time; time.sleep(3)
" 2>&1
    sleep 1

    local out
    out=$(capture hangon read nopty)
    if echo "$out" | grep -q "stdout_marker"; then
        pass "--no-pty captures stdout"
    else
        fail "--no-pty stdout: expected 'stdout_marker', got: $out"
    fi

    hangon stop nopty 2>/dev/null || true
}

test_no_pty_stderr() {
    hangon start process --name noptye --no-pty -- python3 -c "
import sys
sys.stderr.write('stderr_marker\n')
sys.stderr.flush()
import time; time.sleep(3)
" 2>&1
    sleep 1

    local out
    out=$(capture hangon stderr noptye)
    if echo "$out" | grep -q "stderr_marker"; then
        pass "--no-pty captures stderr separately"
    else
        fail "--no-pty stderr: expected 'stderr_marker', got: $out"
    fi

    hangon stop noptye 2>/dev/null || true
}

# --- Tests: Process wait ---

test_process_wait() {
    hangon start process --name waiter -- python3 -c "
import time
print('started')
time.sleep(1)
print('done')
" 2>&1
    sleep 0.3

    local out code=0
    out=$(capture hangon wait waiter) || code=$?
    if echo "$out" | grep -q "exit code: 0"; then
        pass "wait returns exit code 0 for clean exit"
    else
        fail "wait: expected 'exit code: 0', got (exit=$code): $out"
    fi

    hangon stop waiter 2>/dev/null || true
}

test_process_wait_nonzero() {
    hangon start process --name failwait -- python3 -c "
import sys
sys.exit(42)
" 2>&1
    sleep 0.5

    local out code=0
    out=$(hangon wait failwait 2>&1) || code=$?
    # The wait command exits with the process exit code.
    if [ "$code" -eq 42 ]; then
        pass "wait propagates non-zero exit code"
    else
        fail "wait nonzero: expected exit 42, got $code (output: $out)"
    fi

    hangon stop failwait 2>/dev/null || true
}

# --- Tests: Session re-use after stop ---

test_session_reuse() {
    hangon start process --name reuse -- python3 -i 2>&1
    hangon expect reuse ">>>" --timeout 10 >/dev/null 2>&1
    hangon stop reuse 2>&1
    sleep 0.5

    # Should be able to start a new session with the same name.
    hangon start process --name reuse -- python3 -i 2>&1
    hangon expect reuse ">>>" --timeout 10 >/dev/null 2>&1

    local out
    out=$(capture hangon list)
    if echo "$out" | grep -q "reuse"; then
        pass "session name can be reused after stop"
    else
        fail "reuse: session not found after restart: $out"
    fi

    hangon stop reuse 2>/dev/null
}

# --- Tests: Send with special characters ---

test_send_special_chars() {
    hangon start process --name special -- python3 -i 2>&1
    hangon expect special ">>>" --timeout 10 >/dev/null 2>&1

    # Test that quotes and special chars survive the send path.
    hangon sendline special "print('hello \"world\"')" >/dev/null 2>&1
    local out
    out=$(capture hangon expect special 'hello "world"' --timeout 5) || true
    if echo "$out" | grep -q 'hello "world"'; then
        pass "send handles special characters"
    else
        fail "special chars: got: $out"
    fi

    hangon stop special 2>/dev/null
}

# --- Tests: macOS app backend (darwin only) ---

test_macos_screenshot() {
    if [ "$(uname -s)" != "Darwin" ]; then
        pass "macOS screenshot (skipped: not darwin)"
        return
    fi

    # Check if screencapture works (requires Screen Recording permission).
    local sc_test="$TEST_HOME/sc-perm-test.png"
    if ! screencapture -x "$sc_test" 2>/dev/null || [ ! -f "$sc_test" ]; then
        pass "macOS screenshot (skipped: screencapture needs Screen Recording permission)"
        rm -f "$sc_test"
        return
    fi
    rm -f "$sc_test"

    # Launch TextEdit via hangon.
    hangon start macos --name textedit TextEdit 2>&1
    sleep 2

    # Alive check.
    local code=0
    hangon alive textedit >/dev/null 2>&1 || code=$?
    if [ "$code" -eq 0 ]; then
        pass "macOS app alive after launch"
    else
        fail "macOS alive: expected exit 0, got $code"
    fi

    # Screenshot.
    local screenshot_file="$TEST_HOME/textedit-shot.png"
    local out
    out=$(capture hangon screenshot textedit "$screenshot_file") || true
    if [ -f "$screenshot_file" ]; then
        local size
        size=$(wc -c < "$screenshot_file")
        if [ "$size" -gt 1000 ]; then
            pass "macOS screenshot created ($size bytes)"
        else
            fail "macOS screenshot: file too small ($size bytes)"
        fi
    else
        fail "macOS screenshot: file not created (output: $out)"
    fi

    # Status should show macos type.
    out=$(capture hangon status textedit)
    if echo "$out" | grep -q "macos"; then
        pass "macOS session status shows type"
    else
        fail "macOS status: expected 'macos', got: $out"
    fi

    hangon stop textedit 2>/dev/null
    sleep 1
}

# --- Main ---

echo ""
echo -e "${BOLD}======================================${NC}"
echo -e "${BOLD}  hangon end-to-end test suite${NC}"
echo -e "${BOLD}======================================${NC}"
echo ""

setup
trap teardown EXIT

TESTS=(
    # CLI basics
    test_version
    test_help
    test_subcommand_help
    test_unknown_command

    # Process backend
    test_process_start_stop
    test_process_sendline_expect
    test_process_read
    test_process_readall
    test_process_screen
    test_process_alive
    test_process_keys
    test_process_status
    test_process_screenshot
    test_expect_timeout
    test_expect_sequential
    test_expect_no_stale_match
    test_read_after_expect
    test_process_wait
    test_process_wait_nonzero

    # Named sessions
    test_named_sessions
    test_default_session
    test_duplicate_name
    test_stopall
    test_session_reuse

    # Error handling
    test_nonexistent_session
    test_bad_args

    # TCP backend
    test_tcp_backend
    test_tcp_read

    # --no-pty mode
    test_no_pty_stdout
    test_no_pty_stderr

    # macOS app backend
    test_macos_screenshot

    # Misc
    test_send_special_chars
)

for t in "${TESTS[@]}"; do
    run_test "$t"
done

echo ""
echo -e "${BOLD}======================================${NC}"
echo -e "  ${GREEN}$PASS passed${NC}, ${RED}$FAIL failed${NC} ($(( PASS + FAIL )) total)"
echo -e "${BOLD}======================================${NC}"

if [ ${#ERRORS[@]} -gt 0 ]; then
    echo ""
    echo -e "${RED}Failures:${NC}"
    for e in "${ERRORS[@]}"; do
        echo -e "  ${RED}-${NC} $e"
    done
fi

echo ""
if [ "$FAIL" -gt 0 ]; then
    exit 1
else
    exit 0
fi
