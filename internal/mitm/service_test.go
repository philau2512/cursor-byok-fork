package mitm

import "testing"

func TestIsExpectedClientDisconnectLog(t *testing.T) {
	for _, message := range []string{
		"Cannot read TLS request from mitm'd client: wsarecv: An existing connection was forcibly closed by the remote host.",
		"Cannot write TLS response body: wsasend: An established connection was aborted by the software in your host machine.",
		"write tcp: broken pipe",
		"read tcp: connection reset by peer",
	} {
		if !isExpectedClientDisconnectLog(message) {
			t.Fatalf("expected disconnect log to be filtered: %q", message)
		}
	}
}

func TestIsExpectedClientDisconnectLogKeepsUnexpectedFailures(t *testing.T) {
	for _, message := range []string{
		"TLS handshake error: certificate verify failed",
		"proxy connection refused",
		"unexpected EOF while parsing response",
	} {
		if isExpectedClientDisconnectLog(message) {
			t.Fatalf("unexpected proxy failure was filtered: %q", message)
		}
	}
}
