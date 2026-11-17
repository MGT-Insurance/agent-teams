package verbs

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	uiShotTimeout  = 30 * time.Second
	uiShotMaxBytes = 10 << 20 // the ateam gate --attach cap
)

// Reasons a screenshot is listed under "missing:" in verdict.md.
const (
	shotExpired         = "expired"
	shotNotPNG          = "not-png"
	shotTooLarge        = "too-large"
	shotNetwork         = "network"
	shotNoLinks         = "no-links"
	shotSigningDisabled = "signing-disabled"
	shotBadTranscript   = "bad-transcript"
)

// uiTranscriptName is the artifact name of an api run's request/response record.
const uiTranscriptName = "transcript.json"

// uiShotResult is the outcome of the artifact step.
type uiShotResult struct {
	Paths   []string // absolute PNG paths, one per downloaded artifact
	Missing []string // one "<name> <reason>" line per failure; never a URL
	// ShotMissing counts the Missing entries that are screenshots, which the ui
	// evidence rule checks; a missing transcript does not fail a ui run.
	ShotMissing int
	// Transcript is the absolute path of the downloaded transcript.json, or "".
	Transcript string
	// TranscriptCalls is the number of calls in Transcript.
	TranscriptCalls int
	Err             error // a local failure (cannot write under --out), not a link failure
}

// evidencePaths lists every downloaded evidence file: the transcript, then the PNGs.
func (r uiShotResult) evidencePaths() []string {
	var paths []string
	if r.Transcript != "" {
		paths = append(paths, r.Transcript)
	}
	return append(paths, r.Paths...)
}

// satisfies reports whether the downloaded evidence meets kind's rule: api needs
// a transcript with at least one call; ui (and an absent kind) needs at least
// one PNG and no missing PNG.
func (r uiShotResult) satisfies(kind string) bool {
	if kind == uiKindAPI {
		return r.Transcript != "" && r.TranscriptCalls > 0
	}
	return len(r.Paths) > 0 && r.ShotMissing == 0
}

// uiArtifact is one "artifact: <name> <url>" line of the job:ui-test comment.
type uiArtifact struct {
	Name, URL string
}

// uiShotLinks is what the job:ui-test comment says about screenshots.
type uiShotLinks struct {
	Artifacts []uiArtifact
	// Disabled is true for "screenshots: disabled (...)" (no signing secret).
	Disabled bool
}

// uiParseShotLinks finds the newest job:ui-test comment that carries the line
// "run: <runID>" and parses its screenshots: and artifact: lines. found is
// false when no such comment exists.
func uiParseShotLinks(comments []uiTestComment, runID string) (links uiShotLinks, found bool) {
	for i := len(comments) - 1; i >= 0; i-- {
		c := comments[i]
		if c.Author != "job:ui-test" {
			continue
		}
		lines := strings.Split(c.Text, "\n")
		hasRun := false
		for _, l := range lines {
			if m := uiRunLineRE.FindStringSubmatch(strings.TrimSpace(l)); m != nil && m[1] == runID {
				hasRun = true
				break
			}
		}
		if !hasRun {
			continue
		}
		for _, l := range lines {
			l = strings.TrimSpace(l)
			switch {
			case strings.HasPrefix(l, "screenshots: disabled"):
				links.Disabled = true
			case strings.HasPrefix(l, "artifact:"):
				rest := strings.TrimSpace(strings.TrimPrefix(l, "artifact:"))
				cut := strings.LastIndexAny(rest, " \t")
				if cut < 0 {
					continue
				}
				name, link := strings.TrimSpace(rest[:cut]), strings.TrimSpace(rest[cut+1:])
				if name != "" && link != "" {
					links.Artifacts = append(links.Artifacts, uiArtifact{Name: name, URL: link})
				}
			}
		}
		return links, true
	}
	return links, false
}

var unsafeNameRE = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// uiShotFileName reduces an artifact name to a safe local file name: the
// basename, characters outside [A-Za-z0-9._-] replaced by _, a .png suffix
// appended when missing, and -2, -3, ... for a name already in used.
func uiShotFileName(name string, used map[string]bool) string {
	base := path.Base(strings.ReplaceAll(name, `\`, "/"))
	base = unsafeNameRE.ReplaceAllString(base, "_")
	if base == "" || base == "." || base == "/" {
		base = "screenshot"
	}
	if !strings.HasSuffix(base, ".png") {
		base += ".png"
	}
	stem := strings.TrimSuffix(base, ".png")
	out := base
	for n := 2; used[out]; n++ {
		out = stem + "-" + strconv.Itoa(n) + ".png"
	}
	used[out] = true
	return out
}

// uiShotExpired reports whether the link's exp (unix seconds) is before now.
// A link with no readable exp is not treated as expired: the server decides.
func uiShotExpired(link string, now time.Time) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	exp, err := strconv.ParseInt(u.Query().Get("exp"), 10, 64)
	if err != nil {
		return false
	}
	return now.Unix() > exp
}

// uiDownloadShots fetches every artifact link for runID: PNGs into
// <out>/screenshots and transcript.json into <out>. The requests carry no Authorization header and no cookie:
// the signature in the URL is the credential.
//
// A non-nil rt fetches each artifact through the run-tools artifact op instead;
// no link is read, so none can be expired.
func uiDownloadShots(d uiTestDeps, rt *uiRunTools, comments []uiTestComment, runID, out string) uiShotResult {
	var res uiShotResult
	links, found := uiParseShotLinks(comments, runID)
	switch {
	case links.Disabled:
		res.Missing = append(res.Missing, "screenshots "+shotSigningDisabled)
		res.ShotMissing++
		return res
	case !found || len(links.Artifacts) == 0:
		res.Missing = append(res.Missing, "screenshots "+shotNoLinks)
		res.ShotMissing++
		return res
	}
	dir := filepath.Join(out, "screenshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		res.Err = err
		return res
	}
	used := map[string]bool{}
	for _, a := range links.Artifacts {
		isTranscript := path.Base(strings.ReplaceAll(a.Name, `\`, "/")) == uiTranscriptName
		file, dest := uiTranscriptName, filepath.Join(out, uiTranscriptName)
		if !isTranscript {
			file = uiShotFileName(a.Name, used)
			dest = filepath.Join(dir, file)
		}
		var data []byte
		var reason string
		if rt != nil {
			data, reason = uiFetchArtifact(d, !isTranscript, func() (*http.Request, error) { return rt.artifactRequest(runID, a.Name) })
		} else {
			if uiShotExpired(a.URL, d.now()) {
				res.addMissing(file+" "+shotExpired, isTranscript)
				continue
			}
			data, reason = uiFetchArtifact(d, !isTranscript, func() (*http.Request, error) { return http.NewRequest(http.MethodGet, a.URL, nil) })
		}
		if reason == "" && isTranscript {
			var tr struct {
				Calls []json.RawMessage `json:"calls"`
			}
			if json.Unmarshal(data, &tr) != nil {
				reason = shotBadTranscript
			} else {
				res.TranscriptCalls = len(tr.Calls)
			}
		}
		if reason != "" {
			res.addMissing(file+" "+reason, isTranscript)
			continue
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			res.Err = err
			return res
		}
		if isTranscript {
			res.Transcript = dest
		} else {
			res.Paths = append(res.Paths, dest)
		}
	}
	return res
}

func (r *uiShotResult) addMissing(line string, transcript bool) {
	r.Missing = append(r.Missing, line)
	if !transcript {
		r.ShotMissing++
	}
}

// uiFetchArtifact sends the request mk builds with one retry on a 5xx or a
// timeout. It returns the body, or a failure reason (http-<code>, not-png,
// too-large, network). The reason never contains the URL. png requires an
// image/png response; otherwise any content type is accepted.
func uiFetchArtifact(d uiTestDeps, png bool, mk func() (*http.Request, error)) (data []byte, reason string) {
	for attempt := 0; ; attempt++ {
		data, reason, retry := uiFetchOnce(d, png, mk)
		if reason == "" || !retry || attempt == 1 {
			return data, reason
		}
	}
}

func uiFetchOnce(d uiTestDeps, png bool, mk func() (*http.Request, error)) (data []byte, reason string, retry bool) {
	req, err := mk()
	if err != nil {
		return nil, shotNetwork, false
	}
	resp, err := d.httpDo(req)
	if err != nil {
		var ne net.Error
		return nil, shotNetwork, errors.As(err, &ne) && ne.Timeout()
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "http-" + strconv.Itoa(resp.StatusCode), resp.StatusCode >= 500
	}
	if png {
		if mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || mt != "image/png" {
			return nil, shotNotPNG, false
		}
	}
	if resp.ContentLength > uiShotMaxBytes {
		return nil, shotTooLarge, false
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, uiShotMaxBytes+1))
	if err != nil {
		var ne net.Error
		return nil, shotNetwork, errors.As(err, &ne) && ne.Timeout()
	}
	if len(data) > uiShotMaxBytes {
		return nil, shotTooLarge, false
	}
	return data, "", false
}

// defaultUITestHTTP is a plain client: no jar, no default headers.
func defaultUITestHTTP(req *http.Request) (*http.Response, error) {
	return (&http.Client{Timeout: uiShotTimeout}).Do(req)
}
