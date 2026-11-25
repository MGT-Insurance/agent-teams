package po

// Frozen contract types; see doc.go. No logic lives here.

// CommentPrefix starts every PO-authored comment.
const CommentPrefix = "po: note (not Eric's words):"

// ProposalKind is the body header of a PO-to-steward proposal mail.
const ProposalKind = "kind: po-proposal"

// Hosted-store key and ledger prefix templates; <area> etc. are substituted
// by the owning beads.
const (
	KeyClaimTmpl   = "po:<area>:claim:<initiative-id>"
	KeySaidTmpl    = "po:<area>:said:<initiative-id>"
	KeyReadingTmpl = "po:<area>:reading:<feature-id>:<signal-id>:<rfc3339>"
	KeyViewTmpl    = "po:<area>:view"
	LedgerPrefix   = "poledger."
)

// Learnings roles: RoleShared is common to all POs, "po-<area>" is per PO.
const RoleShared = "po"

// Ledger categories added for POs.
const (
	CatNudge        = "nudge"
	CatImpactCall   = "impact-call"
	CatNewWork      = "new-work"
	CatBoldProposal = "bold-proposal"
	CatOverlapCall  = "overlap-call"
	CatCharterChg   = "charter-change"
	CatClaim        = "claim"
	CatPlanFeedback = "plan-feedback"
	CatPRFit        = "pr-fit"
)

// Signal sources.
const (
	SourceWarehouse = "warehouse"
	SourceDatadog   = "datadog"
	SourceLinear    = "linear"
	SourceLangfuse  = "langfuse"
	SourceVercel    = "vercel"
)

// Charter is the YAML front matter of a PO charter file.
type Charter struct {
	Area     string    `yaml:"area"`
	Mission  string    `yaml:"mission"`
	Repos    []string  `yaml:"repos"`
	Features []Feature `yaml:"features"`
	Goals    []Goal    `yaml:"goals"`
	Signals  []Signal  `yaml:"signals"`
	Fences   []Fence   `yaml:"fences"`
	Reach    Reach     `yaml:"reach"`
}

// Feature is a named part of the area and the rules matching work to it.
type Feature struct {
	ID    string       `yaml:"id"`
	Name  string       `yaml:"name"`
	Match FeatureMatch `yaml:"match"`
}

// FeatureMatch lists what ties an initiative to a feature.
type FeatureMatch struct {
	Words    []string `yaml:"words"`
	Repos    []string `yaml:"repos"`
	Paths    []string `yaml:"paths"`
	Services []string `yaml:"services"`
}

// Goal is an outcome the PO steers toward; Signals are signal ids.
type Goal struct {
	ID      string   `yaml:"id"`
	Title   string   `yaml:"title"`
	Done    string   `yaml:"done"`
	Signals []string `yaml:"signals"`
}

// Signal is a measurable reading; Source is one of the Source* values.
type Signal struct {
	ID     string  `yaml:"id"`
	Title  string  `yaml:"title"`
	Source string  `yaml:"source"`
	Query  string  `yaml:"query"`
	Target *Target `yaml:"target,omitempty"`
}

// Target is a signal threshold; Op is ">=" or "<=".
type Target struct {
	Op    string  `yaml:"op"`
	Value float64 `yaml:"value"`
}

// Fence is something the PO must not do, with the reason.
type Fence struct {
	What   string `yaml:"what"`
	Reason string `yaml:"reason"`
}

// Reach says where the PO talks to Eric; Summary defaults to false.
type Reach struct {
	TopicTitle string `yaml:"topic_title"`
	Summary    bool   `yaml:"summary"`
}

// Claim is the value at po:<area>:claim:<initiative-id>. Fit is "clear" or
// "possible".
type Claim struct {
	Fit    string `json:"fit"`
	Reason string `json:"reason"`
	At     string `json:"at"`
	Lead   bool   `json:"lead"`
}

// Said is one element of the list at po:<area>:said:<initiative-id>. To is
// "dri", "steward" or "eric".
type Said struct {
	To   string `json:"to"`
	At   string `json:"at"`
	Gist string `json:"gist"`
}

// Reading is the value at po:<area>:reading:<feature-id>:<signal-id>:<rfc3339>.
type Reading struct {
	Value  string `json:"value"`
	Source string `json:"source"`
	Note   string `json:"note"`
}
