package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestServer returns a server that answers the last request with
// responseFn, recording the decoded request body.
func newTestServer(t *testing.T, responseFn func(w http.ResponseWriter)) (*httptest.Server, *map[string]any) {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/systemone":
			json.NewDecoder(r.Body).Decode(&got)
			w.Header().Set("Content-Type", "application/json")
			responseFn(w)
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"object":"list","data":[{"id":"julia-1","object":"model","owned_by":"me"}]}`))
		case "/props":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"general.architecture":"decoder","modalities":{"vision":true}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func noulBody(p float64) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Write([]byte(`{"model":"julia-1","answers":{"q":{"type":"noul","noul":` +
			jsonNum(p) + `}},"usage":{"input_tokens":7,"output_tokens":1}}`))
	}
}

func jsonNum(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

func run(t *testing.T, args []string, stdin string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = execute(args, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func TestNoul_Pass(t *testing.T) {
	srv, got := newTestServer(t, noulBody(0.9))
	code, out, stderr := run(t, []string{"noul", "--url", srv.URL + "/v1/systemone",
		"--state", "disk at 97%", "--q", "Alert ops?", "--threshold", "0.8"}, "")
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	if env["model"] != "julia-1" {
		t.Errorf("model = %v", env["model"])
	}
	ans := env["answers"].(map[string]any)["q"].(map[string]any)
	if ans["type"] != "noul" || ans["noul"] != 0.9 || ans["passed"] != true {
		t.Errorf("answer = %v", ans)
	}
	usage := env["usage"].(map[string]any)
	if usage["input_tokens"] != float64(7) {
		t.Errorf("usage = %v", usage)
	}
	if _, ok := env["latency_ms"]; !ok {
		t.Error("latency_ms missing")
	}
	// Wire request sanity.
	req := *got
	if req["state"] != "disk at 97%" {
		t.Errorf("state = %v", req["state"])
	}
	q := req["questions"].(map[string]any)["q"].(map[string]any)
	if q["type"] != "noul" {
		t.Errorf("question = %v", q)
	}
}

func TestNoul_FailThreshold(t *testing.T) {
	srv, _ := newTestServer(t, noulBody(0.3))
	code, out, _ := run(t, []string{"noul", "--url", srv.URL + "/v1/systemone",
		"--state", "x", "--q", "y?", "--threshold", "0.8"}, "")
	if code != ExitFailed {
		t.Fatalf("exit = %d, want 1 (out %s)", code, out)
	}
	var env map[string]any
	json.Unmarshal([]byte(out), &env)
	ans := env["answers"].(map[string]any)["q"].(map[string]any)
	if ans["passed"] != false {
		t.Errorf("passed = %v, want false", ans["passed"])
	}
}

func TestChoice_Quiet(t *testing.T) {
	srv, got := newTestServer(t, func(w http.ResponseWriter) {
		w.Write([]byte(`{"model":"m","answers":{"q":{"type":"choice","choice":"tech","probabilities":{"tech":0.8,"billing":0.2},"confidence":0.8}},"usage":{"input_tokens":1,"output_tokens":1}}`))
	})
	code, out, _ := run(t, []string{"choice", "--url", srv.URL + "/v1/systemone",
		"--state", "ticket", "--q", "Category?", "--options", "billing:payments|tech:bugs",
		"--threshold", "0.5", "--quiet"}, "")
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if strings.TrimSpace(out) != "tech" {
		t.Errorf("quiet out = %q, want \"tech\"", out)
	}
	crit := (*got)["questions"].(map[string]any)["q"].(map[string]any)["criteria"].(map[string]any)
	if crit["tech"] != "bugs" || crit["billing"] != "payments" {
		t.Errorf("criteria = %v", crit)
	}
}

func TestScore(t *testing.T) {
	srv, got := newTestServer(t, func(w http.ResponseWriter) {
		w.Write([]byte(`{"model":"m","answers":{"q":{"type":"score","score":2,"legend":{"2":"高"},"probabilities":{"0":0.1,"1":0.2,"2":0.7},"confidence":0.7}},"usage":{"input_tokens":1,"output_tokens":1}}`))
	})
	code, out, _ := run(t, []string{"score", "--url", srv.URL + "/v1/systemone",
		"--state", "ctx", "--q", "Risk?", "--scale", "低,中,高"}, "")
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0 (out %s)", code, out)
	}
	crit := (*got)["questions"].(map[string]any)["q"].(map[string]any)["criteria"]
	if len(crit.([]any)) != 3 {
		t.Errorf("criteria = %v", crit)
	}
	if !strings.Contains(out, `"score":2`) {
		t.Errorf("out = %s", out)
	}
}

func TestBatch(t *testing.T) {
	srv, got := newTestServer(t, func(w http.ResponseWriter) {
		w.Write([]byte(`{"model":"m","answers":{"u":{"type":"noul","noul":0.9},"r":{"type":"choice","choice":"sre","probabilities":{"sre":1},"confidence":1},"c":{"type":"score","score":1,"legend":{"1":"mid"},"probabilities":{"0":0.5,"1":0.5},"confidence":0.5}},"usage":{"input_tokens":3,"output_tokens":3}}`))
	})
	code, out, stderr := run(t, []string{"batch", "--url", srv.URL + "/v1/systemone",
		"--state", "ctx",
		"--noul", "u:Is it urgent?",
		"--choice", "r:Route:sre|frontend",
		"--score", "c:Completeness:low:mid:high",
		"--quiet"}, "")
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr %s)", code, stderr)
	}
	lines := strings.Fields(strings.TrimSpace(out))
	// Three "id value" pairs, ids sorted: c, r, u.
	if len(lines) != 6 || lines[0] != "c" || lines[2] != "r" || lines[4] != "u" {
		t.Errorf("quiet out = %q", out)
	}
	qs := (*got)["questions"].(map[string]any)
	if len(qs) != 3 {
		t.Fatalf("questions = %v", qs)
	}
	crit := qs["c"].(map[string]any)["criteria"].([]any)
	if len(crit) != 3 || crit[2] != "high" {
		t.Errorf("score criteria = %v", crit)
	}
}

func TestAsk_InlineQuestions(t *testing.T) {
	srv, got := newTestServer(t, noulBody(0.6))
	code, _, stderr := run(t, []string{"ask", "--url", srv.URL + "/v1/systemone",
		"--state", "ctx",
		"--questions", `{"q":{"type":"noul","instructions":"Did it work?"}}`}, "")
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr %s)", code, stderr)
	}
	q := (*got)["questions"].(map[string]any)["q"].(map[string]any)
	if q["type"] != "noul" || q["instructions"] != "Did it work?" {
		t.Errorf("question = %v", q)
	}
}

func TestState_JSONFile(t *testing.T) {
	srv, got := newTestServer(t, noulBody(0.9))
	dir := t.TempDir()
	p := filepath.Join(dir, "ctx.json")
	if err := os.WriteFile(p, []byte(`{"alert":"disk_full","pct":97}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := run(t, []string{"noul", "--url", srv.URL + "/v1/systemone",
		"--state", "@" + p, "--q", "Restart?"}, "")
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr %s)", code, stderr)
	}
	state := (*got)["state"].(map[string]any)
	if state["alert"] != "disk_full" {
		t.Errorf("state = %v", state)
	}
}

func TestState_Stdin(t *testing.T) {
	srv, got := newTestServer(t, noulBody(0.9))
	code, _, stderr := run(t, []string{"noul", "--url", srv.URL + "/v1/systemone",
		"--state", "-", "--q", "x?"}, "state from stdin")
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr %s)", code, stderr)
	}
	if (*got)["state"] != "state from stdin" {
		t.Errorf("state = %v", (*got)["state"])
	}
}

func TestUsageErrors(t *testing.T) {
	srv, _ := newTestServer(t, noulBody(0.9))
	cases := []struct {
		name string
		args []string
	}{
		{"missing state", []string{"noul", "--url", srv.URL + "/v1/systemone", "--q", "x?"}},
		{"missing q", []string{"noul", "--url", srv.URL + "/v1/systemone", "--state", "s"}},
		{"missing options", []string{"choice", "--url", srv.URL + "/v1/systemone", "--state", "s", "--q", "x?"}},
		{"bad scale", []string{"score", "--url", srv.URL + "/v1/systemone", "--state", "s", "--q", "x?", "--scale", "only"}},
		{"empty batch", []string{"batch", "--url", srv.URL + "/v1/systemone", "--state", "s"}},
		{"unknown command", []string{"nope"}},
		{"no command", nil},
	}
	for _, tc := range cases {
		code, _, stderr := run(t, tc.args, "")
		if code != ExitUsage {
			t.Errorf("%s: exit = %d, want 2 (stderr %s)", tc.name, code, stderr)
		}
		if !strings.Contains(stderr, "sysone:") {
			t.Errorf("%s: stderr = %q", tc.name, stderr)
		}
	}
}

func TestRequiredFlags(t *testing.T) {
	// Help marks mandatory flags, and missing ones report the flag name with exit 2.
	code, stdout, _ := run(t, []string{"noul", "--help"}, "")
	if code != ExitOK || !strings.Contains(stdout, "[required]") {
		t.Fatalf("noul --help: exit = %d, help missing [required]:\n%s", code, stdout)
	}
	code, _, stderr := run(t, []string{"noul", "--url", "http://127.0.0.1:1/v1/systemone"}, "")
	if code != ExitUsage || !strings.Contains(stderr, `"state"`) || !strings.Contains(stderr, `"q"`) {
		t.Errorf("missing flags: exit = %d, stderr = %q", code, stderr)
	}
}

func TestExitCode_Transport_503(t *testing.T) {
	srv, _ := newTestServer(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"code":503,"message":"loading","type":"server_error"}}`))
	})
	code, _, _ := run(t, []string{"noul", "--url", srv.URL + "/v1/systemone",
		"--state", "s", "--q", "x?"}, "")
	if code != ExitTransport {
		t.Fatalf("exit = %d, want 3", code)
	}
}

func TestExitCode_Contract_400(t *testing.T) {
	srv, _ := newTestServer(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"code":400,"message":"bad state","type":"invalid_request_error"}}`))
	})
	code, _, _ := run(t, []string{"noul", "--url", srv.URL + "/v1/systemone",
		"--state", "s", "--q", "x?"}, "")
	if code != ExitContract {
		t.Fatalf("exit = %d, want 4", code)
	}
}

func TestExitCode_Transport_Connection(t *testing.T) {
	code, _, stderr := run(t, []string{"noul", "--url", "http://127.0.0.1:1/v1/systemone",
		"--state", "s", "--q", "x?", "--timeout", "2s"}, "")
	if code != ExitTransport {
		t.Fatalf("exit = %d, want 3 (stderr %s)", code, stderr)
	}
}

func TestProbes(t *testing.T) {
	srv, _ := newTestServer(t, noulBody(0.9))
	base := srv.URL

	code, out, stderr := run(t, []string{"health", "--url", base + "/v1/systemone"}, "")
	if code != ExitOK || !strings.Contains(out, `"status":"ok"`) {
		t.Errorf("health: exit = %d out = %s stderr = %s", code, out, stderr)
	}

	code, out, _ = run(t, []string{"models", "--url", base + "/v1/systemone"}, "")
	if code != ExitOK || !strings.Contains(out, `"id":"julia-1"`) {
		t.Errorf("models: exit = %d out = %s", code, out)
	}

	code, out, _ = run(t, []string{"props", "--url", base + "/v1/systemone"}, "")
	if code != ExitOK || !strings.Contains(out, `"vision":true`) {
		t.Errorf("props: exit = %d out = %s", code, out)
	}

	// Connection failure on health maps to transport exit code.
	code, _, _ = run(t, []string{"health", "--url", "http://127.0.0.1:1/v1/systemone", "--timeout", "2s"}, "")
	if code != ExitTransport {
		t.Errorf("health down: exit = %d, want 3", code)
	}
}

func TestAgentHelp(t *testing.T) {
	code, out, _ := run(t, []string{"agent-help"}, "")
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	var spec map[string]any
	if err := json.Unmarshal([]byte(out), &spec); err != nil {
		t.Fatalf("agent-help is not JSON: %v", err)
	}
	for _, key := range []string{"commands", "exit_codes", "flags", "examples", "contract"} {
		if _, ok := spec[key]; !ok {
			t.Errorf("agent-help missing %q", key)
		}
	}
	codes := spec["exit_codes"].(map[string]any)
	if codes["3"] == nil || codes["4"] == nil {
		t.Error("exit codes 3/4 missing")
	}
}

func TestPretty(t *testing.T) {
	srv, _ := newTestServer(t, noulBody(0.9))
	code, out, _ := run(t, []string{"noul", "--url", srv.URL + "/v1/systemone",
		"--state", "s", "--q", "x?", "--pretty"}, "")
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "\n  \"model\"") {
		t.Errorf("pretty output not indented:\n%s", out)
	}
}

func TestKeyShorthand(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"m","answers":{"q":{"type":"noul","noul":0.9}},"usage":{"input_tokens":1,"output_tokens":0}}`))
	}))
	t.Cleanup(srv.Close)
	code, _, stderr := run(t, []string{"noul", "--url", srv.URL + "/v1/systemone",
		"--key", "sekret", "--state", "s", "--q", "x?"}, "")
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr %s)", code, stderr)
	}
	if auth != "Bearer sekret" {
		t.Errorf("Authorization = %q", auth)
	}
}
