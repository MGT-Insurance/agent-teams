package po

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Violation is one failed charter rule. Rule is a stable name.
type Violation struct {
	Rule string `json:"rule"`
	Msg  string `json:"msg"`
}

// Rule names reported by CheckCharter.
const (
	RuleParse        = "parse"
	RuleGoalRequired = "goal-required"
	RuleSignalUnres  = "signal-unresolved"
	RuleIDDuplicate  = "id-duplicate"
	RuleBacklogKey   = "backlog-key"
	RuleIDRequired   = "id-required"
	RuleAreaInvalid  = "area-invalid"
	RuleSignalSource = "signal-source"
	RuleTargetOp     = "target-op"
	RuleTargetValue  = "target-value"
)

// backlogKeys are top-level front matter keys that make a charter a task
// list, which it must not be.
var backlogKeys = map[string]bool{
	"backlog": true, "tasks": true, "task_list": true, "tasklist": true,
	"todo": true, "todos": true, "task_backlog": true, "work_items": true,
}

var signalSources = map[string]bool{"warehouse": true, "datadog": true, "linear": true, "langfuse": true, "vercel": true}

// ParseCharter parses a charter markdown file (YAML front matter then prose)
// into a Charter. The second result lists the top-level front matter keys in
// file order, which CheckCharter needs for the backlog rule.
func ParseCharter(data []byte) (*Charter, []string, error) {
	fm, err := frontMatter(string(data))
	if err != nil {
		return nil, nil, err
	}
	root, err := parseYAML(fm)
	if err != nil {
		return nil, nil, err
	}
	m, ok := root.(*ymap)
	if !ok {
		return nil, nil, fmt.Errorf("front matter is not a mapping")
	}
	c, err := decodeCharter(m)
	if err != nil {
		return nil, nil, err
	}
	return c, m.keys, nil
}

// CheckCharter parses data and returns the charter (nil on parse failure)
// with every rule violation, in a stable order.
func CheckCharter(data []byte) (*Charter, []Violation) {
	c, keys, err := ParseCharter(data)
	if err != nil {
		return nil, []Violation{{RuleParse, err.Error()}}
	}
	return c, Validate(c, keys)
}

// Validate applies the contract rules (doc.go section 1) to a parsed charter.
func Validate(c *Charter, topKeys []string) []Violation {
	var v []Violation
	add := func(rule, f string, a ...any) { v = append(v, Violation{rule, fmt.Sprintf(f, a...)}) }

	if err := ValidateArea(c.Area); err != nil {
		add(RuleAreaInvalid, "%v", err)
	}
	if len(c.Goals) == 0 {
		add(RuleGoalRequired, "charter has no goals; at least one goal is required")
	}
	for _, k := range topKeys {
		if backlogKeys[strings.ToLower(k)] {
			add(RuleBacklogKey, "front matter key %q is a task backlog; a charter is not a task list", k)
		}
	}
	dup := func(kind string, ids []string) {
		seen := map[string]bool{}
		for i, id := range ids {
			if id == "" {
				add(RuleIDRequired, "%s #%d has no id", kind, i+1)
				continue
			}
			if seen[id] {
				add(RuleIDDuplicate, "duplicate %s id %q", kind, id)
			}
			seen[id] = true
		}
	}
	var fi, gi, si []string
	for _, f := range c.Features {
		fi = append(fi, f.ID)
	}
	for _, g := range c.Goals {
		gi = append(gi, g.ID)
	}
	sigs := map[string]bool{}
	for _, s := range c.Signals {
		si = append(si, s.ID)
		sigs[s.ID] = true
	}
	dup("feature", fi)
	dup("goal", gi)
	dup("signal", si)
	for _, s := range c.Signals {
		if !signalSources[s.Source] {
			add(RuleSignalSource, "signal %q source %q: want warehouse, datadog, linear, langfuse or vercel", s.ID, s.Source)
		}
		if s.Target != nil {
			if s.Target.Op != ">=" && s.Target.Op != "<=" {
				add(RuleTargetOp, "signal %q target op %q: want >= or <=", s.ID, s.Target.Op)
			}
			if math.IsNaN(s.Target.Value) || math.IsInf(s.Target.Value, 0) {
				add(RuleTargetValue, "signal %q target value must be a finite number", s.ID)
			}
		}
	}
	for _, g := range c.Goals {
		for _, id := range g.Signals {
			if !sigs[id] {
				add(RuleSignalUnres, "goal %q references unknown signal id %q", g.ID, id)
			}
		}
	}
	return v
}

// Summary is a one-line description of a valid charter.
func Summary(c *Charter) string {
	return fmt.Sprintf("area=%s repos=%d features=%d goals=%d signals=%d fences=%d",
		c.Area, len(c.Repos), len(c.Features), len(c.Goals), len(c.Signals), len(c.Fences))
}

func frontMatter(s string) (string, error) {
	s = strings.TrimPrefix(s, "\ufeff")
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return "", fmt.Errorf("no YAML front matter: file must start with a --- line")
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") == "---" {
			return strings.Join(lines[1:i], "\n"), nil
		}
	}
	return "", fmt.Errorf("unterminated YAML front matter: no closing --- line")
}

// ---- decoding the generic tree into Charter ----

func decodeCharter(m *ymap) (*Charter, error) {
	var c Charter
	var err error
	// Every field read records the first error; none is silently dropped.
	str := func(y *ymap, key string) string {
		v, e := y.str(key)
		if err == nil {
			err = e
		}
		return v
	}
	list := func(y *ymap, key string) []string {
		v, e := y.strList(key)
		if err == nil {
			err = e
		}
		return v
	}
	sub := func(y *ymap, key string) *ymap {
		switch v := y.m[key].(type) {
		case nil:
			return nil
		case *ymap:
			return v
		}
		if err == nil {
			err = fmt.Errorf("%q must be a mapping", key)
		}
		return nil
	}
	c.Area = str(m, "area")
	c.Mission = str(m, "mission")
	c.Repos = list(m, "repos")
	for _, e := range m.maps("features", &err) {
		var f Feature
		f.ID = str(e, "id")
		f.Name = str(e, "name")
		if mm := sub(e, "match"); mm != nil {
			f.Match.Words = list(mm, "words")
			f.Match.Repos = list(mm, "repos")
			f.Match.Paths = list(mm, "paths")
			f.Match.Services = list(mm, "services")
		}
		c.Features = append(c.Features, f)
	}
	for _, e := range m.maps("goals", &err) {
		var g Goal
		g.ID = str(e, "id")
		g.Title = str(e, "title")
		g.Done = str(e, "done")
		g.Signals = list(e, "signals")
		c.Goals = append(c.Goals, g)
	}
	for _, e := range m.maps("signals", &err) {
		var s Signal
		s.ID = str(e, "id")
		s.Title = str(e, "title")
		s.Source = str(e, "source")
		s.Query = str(e, "query")
		if t := sub(e, "target"); t != nil {
			op := str(t, "op")
			val := str(t, "value")
			f, perr := strconv.ParseFloat(val, 64)
			if perr != nil {
				return nil, fmt.Errorf("signal %q target value %q is not a number", s.ID, val)
			}
			s.Target = &Target{Op: op, Value: f}
		}
		c.Signals = append(c.Signals, s)
	}
	for _, e := range m.maps("fences", &err) {
		var f Fence
		f.What = str(e, "what")
		f.Reason = str(e, "reason")
		c.Fences = append(c.Fences, f)
	}
	if r := sub(m, "reach"); r != nil {
		c.Reach.TopicTitle = str(r, "topic_title")
		if sv := str(r, "summary"); sv != "" {
			switch strings.ToLower(sv) {
			case "true", "yes":
				c.Reach.Summary = true
			case "false", "no":
			default:
				return nil, fmt.Errorf("reach.summary %q is not a boolean", sv)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ---- minimal YAML subset parser ----
//
// Supports what charters use: block mappings and sequences, "- key: v" items,
// flow lists and maps, quoted and plain scalars, | and > block scalars, and
// comments. Anchors, tags, multi-document streams and multi-line plain scalars
// are not supported.

type ymap struct {
	keys []string
	m    map[string]any
}

func (y *ymap) str(key string) (string, error) {
	switch v := y.m[key].(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	}
	return "", fmt.Errorf("%q must be a scalar", key)
}

func (y *ymap) strList(key string) ([]string, error) {
	switch v := y.m[key].(type) {
	case nil:
		return nil, nil
	case string:
		return []string{v}, nil
	case []any:
		var out []string
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("%q must be a list of scalars", key)
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%q must be a list", key)
}

// maps returns the mapping elements of a list key; a non-list or non-mapping
// element sets *err.
func (y *ymap) maps(key string, err *error) []*ymap {
	v, ok := y.m[key]
	if !ok || v == nil || *err != nil {
		return nil
	}
	l, ok := v.([]any)
	if !ok {
		*err = fmt.Errorf("%q must be a list", key)
		return nil
	}
	var out []*ymap
	for _, e := range l {
		mm, ok := e.(*ymap)
		if !ok {
			*err = fmt.Errorf("%q entries must be mappings", key)
			return nil
		}
		out = append(out, mm)
	}
	return out
}

type yline struct {
	no     int // 0-based index into raw
	indent int
	text   string // comment-stripped, trimmed
}

type yparser struct {
	raw   []string
	lines []yline
	i     int
}

func parseYAML(src string) (any, error) {
	p := &yparser{raw: strings.Split(src, "\n")}
	for n, r := range p.raw {
		if strings.Contains(r, "\t") && strings.TrimLeft(r, " ") != strings.TrimLeft(r, " \t") {
			return nil, fmt.Errorf("line %d: tab used for indentation", n+1)
		}
		t := strings.TrimSpace(stripComment(r))
		if t == "" {
			continue
		}
		p.lines = append(p.lines, yline{n, len(r) - len(strings.TrimLeft(r, " ")), t})
	}
	if len(p.lines) == 0 {
		return &ymap{m: map[string]any{}}, nil
	}
	v, err := p.block(p.lines[0].indent)
	if err != nil {
		return nil, err
	}
	if p.i < len(p.lines) {
		return nil, fmt.Errorf("line %d: unexpected indentation", p.lines[p.i].no+1)
	}
	return v, nil
}

func stripComment(s string) string {
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == '\\' && q == '"' {
				i++
			} else if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			if i == 0 || s[i-1] == ' ' || s[i-1] == '[' || s[i-1] == '{' || s[i-1] == ',' || s[i-1] == ':' {
				q = c
			}
		case c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			return s[:i]
		}
	}
	return s
}

func isDash(t string) bool { return t == "-" || strings.HasPrefix(t, "- ") }

func (p *yparser) block(indent int) (any, error) {
	if isDash(p.lines[p.i].text) {
		return p.seq(indent)
	}
	return p.mapping(indent)
}

func (p *yparser) seq(indent int) (any, error) {
	out := []any{}
	for p.i < len(p.lines) && p.lines[p.i].indent == indent && isDash(p.lines[p.i].text) {
		l := p.lines[p.i]
		rest := strings.TrimSpace(strings.TrimPrefix(l.text, "-"))
		switch {
		case rest == "":
			p.i++
			if p.i < len(p.lines) && p.lines[p.i].indent > indent {
				v, err := p.block(p.lines[p.i].indent)
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			} else {
				out = append(out, nil)
			}
		case splitKey(rest) != -1 && !isFlow(rest):
			off := len(l.text) - len(rest)
			p.lines[p.i] = yline{l.no, indent + off, rest}
			v, err := p.mapping(indent + off)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		default:
			p.i++
			v, err := parseScalarOrFlow(rest, l.no)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
	}
	return out, nil
}

func isFlow(s string) bool { return strings.HasPrefix(s, "[") || strings.HasPrefix(s, "{") }

// splitKey returns the index of the key/value colon in s, or -1.
func splitKey(s string) int {
	if s == "" || s[0] == '"' || s[0] == '\'' {
		if s == "" {
			return -1
		}
		q := s[0]
		for i := 1; i < len(s); i++ {
			if s[i] == '\\' && q == '"' {
				i++
			} else if s[i] == q {
				if i+1 < len(s) && s[i+1] == ':' && (i+2 == len(s) || s[i+2] == ' ') {
					return i + 1
				}
				return -1
			}
		}
		return -1
	}
	for i := 0; i < len(s); i++ {
		if s[i] == ':' && (i+1 == len(s) || s[i+1] == ' ') {
			return i
		}
	}
	return -1
}

func (p *yparser) mapping(indent int) (any, error) {
	y := &ymap{m: map[string]any{}}
	for p.i < len(p.lines) && p.lines[p.i].indent == indent && !isDash(p.lines[p.i].text) {
		l := p.lines[p.i]
		c := splitKey(l.text)
		if c < 0 {
			return nil, fmt.Errorf("line %d: expected \"key: value\"", l.no+1)
		}
		key, err := unquote(strings.TrimSpace(l.text[:c]), l.no)
		if err != nil {
			return nil, err
		}
		if _, dup := y.m[key]; dup {
			return nil, fmt.Errorf("line %d: duplicate key %q", l.no+1, key)
		}
		val := strings.TrimSpace(l.text[c+1:])
		p.i++
		var v any
		switch {
		case val == "":
			if p.i < len(p.lines) && (p.lines[p.i].indent > indent ||
				(p.lines[p.i].indent == indent && isDash(p.lines[p.i].text))) {
				v, err = p.block(p.lines[p.i].indent)
			}
		case val[0] == '|' || val[0] == '>':
			v = p.blockScalar(val[0] == '>', l.no, indent)
		default:
			v, err = parseScalarOrFlow(val, l.no)
		}
		if err != nil {
			return nil, err
		}
		y.keys = append(y.keys, key)
		y.m[key] = v
	}
	return y, nil
}

func (p *yparser) blockScalar(folded bool, keyNo, indent int) string {
	var parts []string
	end := keyNo
	for n := keyNo + 1; n < len(p.raw); n++ {
		r := p.raw[n]
		if strings.TrimSpace(r) != "" && len(r)-len(strings.TrimLeft(r, " ")) <= indent {
			break
		}
		parts = append(parts, strings.TrimSpace(r))
		end = n
	}
	for p.i < len(p.lines) && p.lines[p.i].no <= end {
		p.i++
	}
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if folded {
		return strings.Join(parts, " ")
	}
	return strings.Join(parts, "\n")
}

func unquote(s string, no int) (string, error) {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		u, err := strconv.Unquote(s)
		if err != nil {
			return "", fmt.Errorf("line %d: bad double-quoted string %s", no+1, s)
		}
		return u, nil
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), nil
	}
	if len(s) > 0 && (s[0] == '"' || s[0] == '\'') {
		return "", fmt.Errorf("line %d: unterminated quoted string", no+1)
	}
	return s, nil
}

func parseScalarOrFlow(s string, no int) (any, error) {
	if !isFlow(s) {
		return unquote(s, no)
	}
	v, rest, err := parseFlow(s, no)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(rest) != "" {
		return nil, fmt.Errorf("line %d: trailing text after flow value", no+1)
	}
	return v, nil
}

func parseFlow(s string, no int) (any, string, error) {
	s = strings.TrimLeft(s, " ")
	if s == "" {
		return nil, "", fmt.Errorf("line %d: unterminated flow value", no+1)
	}
	open := s[0]
	if open != '[' && open != '{' {
		return flowScalar(s, no)
	}
	closer := byte(']')
	if open == '{' {
		closer = '}'
	}
	s = s[1:]
	list := []any{}
	y := &ymap{m: map[string]any{}}
	for {
		s = strings.TrimLeft(s, " ")
		if s == "" {
			return nil, "", fmt.Errorf("line %d: unterminated flow value", no+1)
		}
		if s[0] == closer {
			s = s[1:]
			break
		}
		if s[0] == ',' {
			s = s[1:]
			continue
		}
		if open == '[' {
			v, rest, err := parseFlow(s, no)
			if err != nil {
				return nil, "", err
			}
			list = append(list, v)
			s = rest
			continue
		}
		k, rest, err := flowScalar(s, no)
		if err != nil {
			return nil, "", err
		}
		rest = strings.TrimLeft(rest, " ")
		if rest == "" || rest[0] != ':' {
			return nil, "", fmt.Errorf("line %d: expected ':' in flow mapping", no+1)
		}
		var v any = ""
		if t := strings.TrimLeft(rest[1:], " "); t != "" && t[0] != ',' && t[0] != '}' {
			v, rest, err = parseFlow(t, no)
			if err != nil {
				return nil, "", err
			}
		} else {
			rest = t
		}
		ks, _ := k.(string)
		y.keys = append(y.keys, ks)
		y.m[ks] = v
		s = rest
	}
	if open == '[' {
		return list, s, nil
	}
	return y, s, nil
}

func flowScalar(s string, no int) (any, string, error) {
	if s[0] == '"' || s[0] == '\'' {
		q := s[0]
		for i := 1; i < len(s); i++ {
			if s[i] == '\\' && q == '"' {
				i++
			} else if s[i] == q {
				if q == '\'' && i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				u, err := unquote(s[:i+1], no)
				return u, s[i+1:], err
			}
		}
		return nil, "", fmt.Errorf("line %d: unterminated quoted string", no+1)
	}
	end := len(s)
	for i := 0; i < len(s); i++ {
		if s[i] == ',' || s[i] == ']' || s[i] == '}' || (s[i] == ':' && (i+1 == len(s) || s[i+1] == ' ')) {
			end = i
			break
		}
	}
	if end == 0 {
		// No progress is possible; callers would loop forever on this.
		return nil, "", fmt.Errorf("line %d: unexpected %q in flow value", no+1, s[:1])
	}
	return strings.TrimSpace(s[:end]), s[end:], nil
}
