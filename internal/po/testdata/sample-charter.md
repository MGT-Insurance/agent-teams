---
area: quoting
mission: >
  Make quoting fast and correct for agents and brokers,
  with measurable latency and accuracy.
repos: [mgt-insurance/midgard, mgt-insurance/dispatcher]
features:
  - id: rating
    name: Rating engine
    match:
      words: [rating, rate, premium]
      repos: [mgt-insurance/midgard]
      paths: ["apps/shadowfax/**"]
      services: [shadowfax]
  - id: bind
    name: "Bind flow"
    match:
      words: [bind]
goals:
  - id: g-latency
    title: Quotes return quickly
    done: p95 quote latency under 5s for a week
    signals: [quote-p95]
  - id: g-accuracy
    title: Quotes are accurate
    done: Accuracy at or above target
    signals:
      - quote-accuracy
      - quote-p95
signals:
  - id: quote-p95
    title: Quote p95 latency seconds
    source: datadog
    query: "p95:quote.latency{*}"  # seconds
    target: {op: "<=", value: 5}
  - id: quote-accuracy
    title: Quote accuracy
    source: warehouse
    query: SELECT avg(accurate) FROM quotes
    target:
      op: ">="
      value: 0.95
fences:
  - what: Never change rates
    reason: Actuarial owns them
reach:
  topic_title: "PO: Quoting"
  summary: true
---

# Quoting PO charter

Prose context only; nothing here is parsed.
