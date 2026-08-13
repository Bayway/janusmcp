package broker

// Multi round-trip requests (SEP-2322) let an upstream answer a tool call with
// `input_required` plus an opaque `requestState` that the client must echo back
// on the retry. The broker relays both, which raises a routing problem the
// upstream cannot see: the retry arrives as an independent request, and the only
// thing tying it to an account is the exposed tool name.
//
// That is not enough. applyActiveTools rebuilds the route table on every account
// switch, and two accounts of the same service export identical tool names
// (namespacing only kicks in for collisions within a single active selector). A
// switch landing between the input_required response and the retry would send
// the user's input responses — which routinely carry freshly typed secrets — to
// the wrong tenant.
//
// So the broker never hands its upstream's state to the client raw. It wraps it
// in a signed envelope naming the account and tool it belongs to, and refuses
// any retry whose envelope disagrees with the route resolved for the call.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// stateEnvelopePrefix marks a requestState minted by this broker. Anything
// without it is passed through untouched (see unwrapRequestState).
const stateEnvelopePrefix = "janus1:"

// stateTagLen is the number of HMAC bytes retained. 16 bytes is well beyond what
// is needed to stop tampering with a loopback-only broker.
const stateTagLen = 16

// stateKey signs request-state envelopes. It lives for the life of the process
// rather than on Core: Core is built as a bare struct literal in main.go and in
// several tests, so a new required field there would be a breaking change for
// no benefit. A restart invalidating in-flight retries is acceptable — the
// remedy is to re-run the call, and unwrapRequestState says so.
var stateKey = mustRandomKey()

func mustRandomKey() []byte {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		panic("janusmcp: cannot seed request-state key: " + err.Error())
	}
	return k
}

// errStateExpired reports an envelope this process cannot have minted, which in
// practice means the broker restarted between the input request and the retry.
var errStateExpired = errors.New("retry state expired (the broker restarted): re-run the call")

// stateEnvelope binds an upstream's opaque state to the route that produced it.
type stateEnvelope struct {
	V    int    `json:"v"`
	Acc  string `json:"acc"`
	Tool string `json:"tool"`
	St   string `json:"st"`
}

// wrapRequestState seals an upstream requestState for the given route. An empty
// upstream state still gets an envelope: the account binding is worth carrying
// even when the upstream keeps no state of its own.
func wrapRequestState(account, tool, upstreamState string) string {
	payload, err := json.Marshal(stateEnvelope{V: 1, Acc: account, Tool: tool, St: upstreamState})
	if err != nil {
		// The struct is all strings and an int; marshalling cannot fail. Degrade
		// to passing the upstream value through rather than dropping the call.
		return upstreamState
	}
	enc := base64.RawURLEncoding.EncodeToString(payload)
	return stateEnvelopePrefix + enc + "." + base64.RawURLEncoding.EncodeToString(signState(payload))
}

// unwrapRequestState recovers the upstream state a client echoed back, checking
// that the envelope belongs to the route now resolved for the call.
//
// A state without the broker's prefix is returned verbatim: it may come from a
// hand-written client or from a broker version that predates the envelope, and
// rejecting it would break calls that are mid-flight across an upgrade.
func unwrapRequestState(state, account, tool string) (string, error) {
	if state == "" || !strings.HasPrefix(state, stateEnvelopePrefix) {
		return state, nil
	}
	encoded, tag, ok := strings.Cut(strings.TrimPrefix(state, stateEnvelopePrefix), ".")
	if !ok {
		return "", errStateExpired
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", errStateExpired
	}
	gotTag, err := base64.RawURLEncoding.DecodeString(tag)
	if err != nil {
		return "", errStateExpired
	}
	if !hmac.Equal(gotTag, signState(payload)) {
		return "", errStateExpired
	}

	var env stateEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return "", errStateExpired
	}
	if env.V != 1 {
		return "", errStateExpired
	}
	// The signature proves we minted it; these checks prove it belongs *here*.
	if env.Acc != account || env.Tool != tool {
		return "", fmt.Errorf(
			"retry state belongs to a different tool (%s on %s), but %s now resolves to %s: "+
				"the active account changed mid-call, re-run it",
			env.Tool, env.Acc, tool, account)
	}
	return env.St, nil
}

func signState(payload []byte) []byte {
	mac := hmac.New(sha256.New, stateKey)
	mac.Write(payload)
	return mac.Sum(nil)[:stateTagLen]
}
