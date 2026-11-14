#!/bin/sh
# Fake LLM command for engine docgen tests (spec section 8 command-mode
# contract). Reads $SCV_DOC, echoes the matching fixture markdown from
# testdata/, and exits 0. Test modes via $SCV_FAKE_MODE:
#   fail      always exit 7 with a stderr line
#   fail-once the first invocation (tracked in $SCV_ANALYSIS_DIR) exits 1,
#             later invocations succeed
#   hang      sleep past any engine timeout (spawns a grandchild so the
#             process-group kill is what actually ends it)
#   empty     exit 0 with no stdout (empty output counts as failure)
# When $SCV_CAPTURE_DIR is set, the observed environment, stdin, and
# argument count are captured per doc for test assertions.

set -u

doc=${SCV_DOC:-unknown}
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

if [ -n "${SCV_CAPTURE_DIR:-}" ]; then
    mkdir -p "$SCV_CAPTURE_DIR"
    printf '%s\n' "$doc" > "$SCV_CAPTURE_DIR/doc.$doc"
    env | sort > "$SCV_CAPTURE_DIR/env.$doc"
    cat > "$SCV_CAPTURE_DIR/stdin.$doc"
    printf '%s\n' "$#" > "$SCV_CAPTURE_DIR/argc.$doc"
else
    cat > /dev/null
fi

case ${SCV_FAKE_MODE:-} in
    fail)
        echo "fake-llm: simulated failure for $doc" >&2
        exit 7
        ;;
    fail-once)
        state="$SCV_ANALYSIS_DIR/.fake-llm.state"
        if [ ! -f "$state" ]; then
            printf 'failed\n' > "$state"
            echo "fake-llm: first-call failure for $doc" >&2
            exit 1
        fi
        ;;
    hang)
        sleep 600 &
        wait
        ;;
    empty)
        exit 0
        ;;
esac

cat "$here/testdata/$doc.md"
