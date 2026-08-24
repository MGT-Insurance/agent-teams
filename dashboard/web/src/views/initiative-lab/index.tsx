import { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import {
  initiativeFor,
  initiatives,
  workForInitiative,
  workScenarios,
  type InitiativeScenario,
  type PipelineStage,
  type PullRequestScenario,
  type QueueGroup,
  type WorkScenario,
} from "./scenarios.js";
import "./initiative-lab.css";

type Concept = "pipeline" | "cockpit" | "queue";

const concepts: Array<{ id: Concept; label: string; eyebrow: string }> = [
  { id: "pipeline", label: "Outcome Pipeline", eyebrow: "Compare flow" },
  { id: "cockpit", label: "Initiative Cockpit", eyebrow: "Understand context" },
  { id: "queue", label: "Action Queue", eyebrow: "Choose what is next" },
];

const pipelineStages: Array<{ id: PipelineStage; label: string; hint: string }> = [
  { id: "investigating", label: "Investigating", hint: "Understand the problem" },
  { id: "building", label: "Building", hint: "Make the change" },
  { id: "in-review", label: "In Review", hint: "Get a decision" },
  { id: "ready-to-land", label: "Ready to Land", hint: "Clear the final path" },
  { id: "done", label: "Done", hint: "Landed or concluded" },
];

const queueGroups: Array<{ id: QueueGroup; label: string; hint: string }> = [
  { id: "needs-you", label: "Needs you", hint: "A decision only you can make" },
  { id: "agents-working", label: "Agents working", hint: "Moving while you focus elsewhere" },
  { id: "waiting", label: "Waiting elsewhere", hint: "The next move belongs outside the team" },
  { id: "landed", label: "Recently landed", hint: "Completed outcomes worth a glance" },
];

function isConcept(value: string | null): value is Concept {
  return concepts.some((concept) => concept.id === value);
}

function progressLabel(work: WorkScenario): string {
  return `${work.progress.complete} of ${work.progress.total}`;
}

function Progress({ complete, total, label }: { complete: number; total: number; label?: string }) {
  const value = Math.round((complete / total) * 100);
  return (
    <div className="initiative-lab__progress">
      <div className="initiative-lab__progress-copy">
        <span>{label ?? "Outcome progress"}</span>
        <strong>{complete} of {total}</strong>
      </div>
      <div
        className="initiative-lab__progress-track"
        role="progressbar"
        aria-label={label ?? "Outcome progress"}
        aria-valuemin={0}
        aria-valuemax={total}
        aria-valuenow={complete}
      >
        <span style={{ width: `${value}%` }} />
      </div>
    </div>
  );
}

function PullRequestLinks({ work }: { work: WorkScenario }) {
  if (work.pullRequests.length === 0) {
    return (
      <div className="initiative-lab__pr-group">
        <span>Active effort · no PR yet</span>
      </div>
    );
  }

  return (
    <div className="initiative-lab__pr-group">
      <span>
        PR group · {work.pullRequests.length === 1 ? "1 PR" : `${work.pullRequests.length} coordinated PRs`}
      </span>
      <ul className="initiative-lab__pr-list" aria-label={`PR group for ${work.title}`}>
        {work.pullRequests.map((pullRequest) => (
          <li key={`${pullRequest.repository}:${pullRequest.number}`}>
            <a href={pullRequest.href} target="_blank" rel="noreferrer">
              <span>{pullRequest.repository}</span>
              <strong>PR #{pullRequest.number}</strong>
              {pullRequest.status === "merged" && <em>Merged</em>}
            </a>
          </li>
        ))}
      </ul>
    </div>
  );
}

function ImplementationMetadata({ work }: { work: WorkScenario }) {
  return (
    <details className="initiative-lab__metadata">
      <summary>Implementation metadata</summary>
      <div>
        <span>Sample Bead ids</span>
        <code>{work.implementationIds.join(" · ")}</code>
      </div>
    </details>
  );
}

function AgentLabel({ work }: { work: WorkScenario }) {
  const stateClass = work.agent.state === "paused" || work.agent.state === "finished"
    ? work.agent.state
    : "active";
  return (
    <span className={`initiative-lab__agent initiative-lab__agent--${stateClass}`}>
      <span aria-hidden="true" />
      Agent: {work.agent.name ? `${work.agent.name} · ` : ""}{work.agent.state}
    </span>
  );
}

function SignalFacts({ work, compact = false }: { work: WorkScenario; compact?: boolean }) {
  return (
    <div className={`initiative-lab__facts${compact ? " initiative-lab__facts--compact" : ""}`}>
      <div>
        <span className="initiative-lab__fact-label">Owner</span>
        <strong>{work.owner}</strong>
      </div>
      <div>
        <span className="initiative-lab__fact-label">Agent state</span>
        <AgentLabel work={work} />
      </div>
      <div>
        <span className="initiative-lab__fact-label">Review</span>
        <strong>{work.review}</strong>
      </div>
      <div>
        <span className="initiative-lab__fact-label">Checks</span>
        <strong>{work.checks}</strong>
      </div>
    </div>
  );
}

function NextAction({ work }: { work: WorkScenario }) {
  return (
    <div className="initiative-lab__next-action">
      <span>Next action</span>
      {work.nextAction ? (
        <p>{work.nextAction.text} <strong>— Responsible: {work.nextAction.responsible}</strong></p>
      ) : (
        <p>None <strong>— Responsible: no one; outcome complete</strong></p>
      )}
    </div>
  );
}

function Blocker({ work }: { work: WorkScenario }) {
  return work.blocker ? (
    <div className="initiative-lab__blocker">
      <span>Blocker</span>
      <p>{work.blocker.reason}</p>
      <small>Unblocker: <strong>{work.blocker.unblocker}</strong></small>
    </div>
  ) : (
    <p className="initiative-lab__clear"><span aria-hidden="true">✓</span> No blocker</p>
  );
}

interface PipelineItem {
  key: string;
  work: WorkScenario;
  pullRequest: PullRequestScenario | null;
}

const pipelineItems = workScenarios.flatMap<PipelineItem>((work) => {
  if (work.pullRequests.length === 0) return [{ key: `${work.id}:effort`, work, pullRequest: null }];
  return work.pullRequests.map((pullRequest) => ({
    key: `${work.id}:pr-${pullRequest.number}`,
    work,
    pullRequest,
  }));
});

function matchesPipelineAttentionFilter(item: PipelineItem, needsYouOnly: boolean): boolean {
  return !needsYouOnly || item.work.needsYou;
}

function PipelineCard({
  item,
  selected,
  onSelect,
}: {
  item: PipelineItem;
  selected: boolean;
  onSelect: (trigger: HTMLButtonElement) => void;
}) {
  const { work, pullRequest } = item;
  return (
    <article
      className={`pipeline-card${work.needsYou ? " pipeline-card--needs-you" : ""}${selected ? " pipeline-card--selected" : ""}`}
    >
      <button
        type="button"
        className="pipeline-card__select"
        onClick={(event) => onSelect(event.currentTarget)}
        aria-haspopup="dialog"
        aria-expanded={selected}
      >
        <span className="pipeline-card__topline">
          <span className="pipeline-card__identity">{pullRequest ? `PR #${pullRequest.number}` : "Active effort"}</span>
          {work.needsYou && <span className="pipeline-card__attention">Needs you</span>}
        </span>
        <strong className="pipeline-card__title">{work.title}</strong>
        <AgentLabel work={work} />
        {work.needsYou && work.nextAction && (
          <span className="pipeline-card__next">
            <small>Next action</small>
            <strong>{work.nextAction.text}</strong>
          </span>
        )}
        <span className="pipeline-card__open">Open details</span>
      </button>
    </article>
  );
}

function PipelineDetail({ item, onClose }: { item: PipelineItem; onClose: () => void }) {
  const panelRef = useRef<HTMLElement>(null);
  const { work, pullRequest } = item;

  useEffect(() => {
    panelRef.current?.focus();
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
    }
    document.addEventListener("keydown", closeOnEscape);
    return () => document.removeEventListener("keydown", closeOnEscape);
  }, [onClose]);

  return (
    <aside
      ref={panelRef}
      className="pipeline__detail"
      role="dialog"
      aria-modal="false"
      aria-labelledby="pipeline-detail-title"
      tabIndex={-1}
    >
      <header className="pipeline__detail-heading">
        <div>
          <span>{pullRequest ? `PR #${pullRequest.number}` : "Active effort"}</span>
          <h3 id="pipeline-detail-title">{work.title}</h3>
        </div>
        <button type="button" onClick={onClose} aria-label={`Close details for ${work.title}`}>Close</button>
      </header>
      <p className="pipeline__detail-summary">{work.summary}</p>

      <div className="pipeline__detail-grid">
        <section>
          <span>Initiative</span>
          <strong>{initiativeFor(work).title}</strong>
        </section>
        <section>
          <span>Owner</span>
          <strong>{work.owner}</strong>
        </section>
        <section>
          <span>Review</span>
          <strong>{work.review}</strong>
        </section>
        <section>
          <span>Checks</span>
          <strong>{work.checks}</strong>
        </section>
      </div>

      <div className="pipeline__detail-evidence">
        <NextAction work={work} />
        <Blocker work={work} />
        <Progress {...work.progress} />
        {pullRequest ? (
          <a className="pipeline__detail-pr" href={pullRequest.href} target="_blank" rel="noreferrer">
            {pullRequest.repository} · PR #{pullRequest.number}{pullRequest.status === "merged" ? " · Merged" : ""}
          </a>
        ) : (
          <p className="pipeline__detail-effort">No PR exists yet. This effort may conclude without one.</p>
        )}
      </div>

      <section className="pipeline__timeline" aria-labelledby="pipeline-timeline-heading">
        <span>Timeline</span>
        <h4 id="pipeline-timeline-heading">Recent activity</h4>
        <ol>
          {work.timeline.map((event) => <li key={event}>{event}</li>)}
        </ol>
      </section>
      <ImplementationMetadata work={work} />
    </aside>
  );
}

function PipelineConcept() {
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [needsYouOnly, setNeedsYouOnly] = useState(false);
  const detailTriggerRef = useRef<HTMLButtonElement | null>(null);
  const selected = pipelineItems.find((item) => item.key === selectedKey) ?? null;

  function closeDetail() {
    detailTriggerRef.current?.focus();
    setSelectedKey(null);
  }

  return (
    <section className="pipeline" aria-labelledby="pipeline-heading">
      <div className="initiative-lab__concept-intro">
        <div>
          <span className="initiative-lab__kicker">Concept A · Flow view</span>
          <h2 id="pipeline-heading">Outcome Pipeline</h2>
        </div>
        <p>Scan real PRs and pre-PR efforts by lifecycle. Needs you stays visible without becoming a stage.</p>
      </div>

      <div className="pipeline__controls">
        <label>
          <input
            type="checkbox"
            aria-label="Needs you only"
            checked={needsYouOnly}
            onChange={(event) => {
              const checked = event.currentTarget.checked;
              setNeedsYouOnly(checked);
              if (checked && selected && !selected.work.needsYou) setSelectedKey(null);
            }}
          />
          <span>Needs you only</span>
          <strong>{pipelineItems.filter((item) => item.work.needsYou).length}</strong>
        </label>
        <p>{needsYouOnly ? "Showing attention items in their current lifecycle stage." : "Showing every delivery item."}</p>
      </div>

      <div
        className="pipeline__scroller"
        role="region"
        aria-label="Outcome Pipeline board; scroll horizontally to view all five stages"
        tabIndex={0}
      >
        <div className="pipeline__board">
          {pipelineStages.map((stage) => {
            const stageItems = pipelineItems.filter(
              (item) => item.work.pipelineStage === stage.id && matchesPipelineAttentionFilter(item, needsYouOnly),
            );
            return (
              <section className="pipeline__stage" key={stage.id} aria-labelledby={`stage-${stage.id}`}>
                <header>
                  <div>
                    <span>{stage.hint}</span>
                    <h3 id={`stage-${stage.id}`}>{stage.label}</h3>
                  </div>
                  <strong aria-label={`${stageItems.length} delivery ${stageItems.length === 1 ? "item" : "items"}`}>
                    {stageItems.length}
                  </strong>
                </header>
                <div className="pipeline__stage-line" aria-hidden="true" />
                {stageItems.length ? (
                  <div className="pipeline__cards">
                    {stageItems.map((item) => (
                      <PipelineCard
                        key={item.key}
                        item={item}
                        selected={item.key === selected?.key}
                        onSelect={(trigger) => {
                          detailTriggerRef.current = trigger;
                          setSelectedKey(item.key);
                        }}
                      />
                    ))}
                  </div>
                ) : (
                  <p className="pipeline__empty">No matching items</p>
                )}
              </section>
            );
          })}
        </div>
      </div>

      {selected && <PipelineDetail item={selected} onClose={closeDetail} />}
    </section>
  );
}

function aggregateProgress(initiative: InitiativeScenario) {
  return workForInitiative(initiative.id).reduce(
    (total, work) => ({
      complete: total.complete + work.progress.complete,
      total: total.total + work.progress.total,
    }),
    { complete: 0, total: 0 },
  );
}

function CockpitWork({ work, index }: { work: WorkScenario; index: number }) {
  return (
    <article className="cockpit__work">
      <div className="cockpit__timeline-mark" aria-hidden="true"><span>{index + 1}</span></div>
      <div className="cockpit__work-body">
        <div className="cockpit__work-heading">
          <div>
            <span>Delivery item</span>
            <h4>{work.title}</h4>
          </div>
          <AgentLabel work={work} />
        </div>
        <p>{work.summary}</p>
        <PullRequestLinks work={work} />
        <NextAction work={work} />
        <SignalFacts work={work} />
        <Blocker work={work} />
        <Progress {...work.progress} />
        <ImplementationMetadata work={work} />
      </div>
    </article>
  );
}

function InitiativeCockpit() {
  const [selectedId, setSelectedId] = useState(initiatives[0]?.id ?? "");
  const selected = initiatives.find((initiative) => initiative.id === selectedId) ?? initiatives[0];
  if (!selected) return null;

  const selectedWork = workForInitiative(selected.id);
  const progress = aggregateProgress(selected);
  const now = selectedWork.find((work) => work.nextAction)?.title ?? "Outcome complete";
  const next = selectedWork.find((work) => work.nextAction)?.nextAction?.text ?? "No next action";
  const blockers = selectedWork.filter((work) => work.blocker);

  return (
    <section className="cockpit" aria-labelledby="cockpit-heading">
      <div className="initiative-lab__concept-intro">
        <div>
          <span className="initiative-lab__kicker">Concept B · Context view</span>
          <h2 id="cockpit-heading">Initiative Cockpit</h2>
        </div>
        <p>Select an initiative to understand its purpose, decision horizon, risks, and PR-group sequence.</p>
      </div>

      <div className="cockpit__layout">
        <nav className="cockpit__master" aria-label="Sample initiatives">
          <span className="cockpit__master-label">Initiatives · 3</span>
          {initiatives.map((initiative) => {
            const initiativeProgress = aggregateProgress(initiative);
            const isSelected = initiative.id === selected.id;
            return (
              <button
                key={initiative.id}
                type="button"
                aria-pressed={isSelected}
                onClick={() => setSelectedId(initiative.id)}
              >
                <span>{isSelected ? "Viewing" : "Open"}</span>
                <strong>{initiative.title}</strong>
                <small>{initiativeProgress.complete} of {initiativeProgress.total} outcomes</small>
              </button>
            );
          })}
        </nav>

        <div className="cockpit__detail" aria-live="polite">
          <header className="cockpit__context">
            <span>Initiative context</span>
            <h3>{selected.title}</h3>
            <p>{selected.purpose}</p>
            <Progress {...progress} label="Initiative progress" />
          </header>

          <div className="cockpit__horizon">
            <section>
              <span>Now</span>
              <strong>{now}</strong>
              <p>{selectedWork.length} delivery item{selectedWork.length === 1 ? "" : "s"}</p>
            </section>
            <section>
              <span>Next</span>
              <strong>{next}</strong>
              <p>{selectedWork.find((work) => work.nextAction)?.nextAction?.responsible ?? "No responsible party"}</p>
            </section>
            <section className={blockers.length ? "cockpit__risk" : "cockpit__risk cockpit__risk--clear"}>
              <span>Risks</span>
              <strong>{blockers.length ? blockers[0]?.blocker?.reason : "No open blocker"}</strong>
              <p>{blockers.length ? `Unblocker: ${blockers[0]?.blocker?.unblocker}` : "Nothing is preventing the next step"}</p>
            </section>
          </div>

          <section className="cockpit__timeline" aria-labelledby="timeline-heading">
            <div className="cockpit__timeline-heading">
              <span>Delivery sequence</span>
              <h3 id="timeline-heading">Delivery timeline</h3>
            </div>
            {selectedWork.map((work, index) => <CockpitWork key={work.id} work={work} index={index} />)}
          </section>
        </div>
      </div>
    </section>
  );
}

function QueueItem({ work, priority }: { work: WorkScenario; priority: number }) {
  const initiative = initiativeFor(work);
  return (
    <details className="queue-item">
      <summary>
        <span className="queue-item__priority">{String(priority).padStart(2, "0")}</span>
        <span className="queue-item__identity">
          <small>{initiative.title}</small>
          <strong>{work.title}</strong>
        </span>
        <span className="queue-item__action">
          <small>Next action</small>
          <strong>{work.nextAction?.text ?? "None — outcome complete"}</strong>
          <em>Responsible: {work.nextAction?.responsible ?? "no one; outcome complete"}</em>
        </span>
        <AgentLabel work={work} />
        <span className="queue-item__progress">{progressLabel(work)}</span>
        <span className="queue-item__expand" aria-hidden="true">+</span>
      </summary>
      <div className="queue-item__detail">
        <p>{work.summary}</p>
        <SignalFacts work={work} />
        <Blocker work={work} />
        <Progress {...work.progress} />
        <PullRequestLinks work={work} />
        <ImplementationMetadata work={work} />
      </div>
    </details>
  );
}

function ActionQueue() {
  return (
    <section className="queue" aria-labelledby="queue-heading">
      <div className="initiative-lab__concept-intro">
        <div>
          <span className="initiative-lab__kicker">Concept C · Priority view</span>
          <h2 id="queue-heading">Action Queue</h2>
        </div>
        <p>Start with the next responsible party. Expand an item to inspect evidence without leaving the queue.</p>
      </div>

      <div className="queue__groups">
        {queueGroups.map((group, groupIndex) => {
          const groupWork = workScenarios.filter((work) => work.queueGroup === group.id);
          return (
            <section className="queue__group" key={group.id} aria-labelledby={`queue-${group.id}`}>
              <header>
                <span>{String(groupIndex + 1).padStart(2, "0")}</span>
                <div>
                  <h3 id={`queue-${group.id}`}>{group.label}</h3>
                  <p>{group.hint}</p>
                </div>
                <strong>{groupWork.length}</strong>
              </header>
              <div className="queue__items">
                {groupWork.map((work, index) => (
                  <QueueItem key={work.id} work={work} priority={index + 1} />
                ))}
              </div>
            </section>
          );
        })}
      </div>
    </section>
  );
}

export default function InitiativeLabView() {
  const [searchParams, setSearchParams] = useSearchParams();
  const requestedConcept = searchParams.get("concept");
  const concept: Concept = isConcept(requestedConcept) ? requestedConcept : "pipeline";
  const activeIndex = concepts.findIndex((item) => item.id === concept);

  useEffect(() => {
    if (isConcept(requestedConcept)) return;
    const nextParams = new URLSearchParams(searchParams);
    nextParams.set("concept", "pipeline");
    setSearchParams(nextParams, { replace: true });
  }, [requestedConcept, searchParams, setSearchParams]);

  const activeConcept = useMemo(() => {
    if (concept === "cockpit") return <InitiativeCockpit />;
    if (concept === "queue") return <ActionQueue />;
    return <PipelineConcept />;
  }, [concept]);

  function selectConcept(nextConcept: Concept) {
    const nextParams = new URLSearchParams(searchParams);
    nextParams.set("concept", nextConcept);
    setSearchParams(nextParams);
  }

  function moveConceptFocus(event: React.KeyboardEvent<HTMLDivElement>) {
    if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    let nextIndex = activeIndex;
    if (event.key === "ArrowLeft") nextIndex = (activeIndex - 1 + concepts.length) % concepts.length;
    if (event.key === "ArrowRight") nextIndex = (activeIndex + 1) % concepts.length;
    if (event.key === "Home") nextIndex = 0;
    if (event.key === "End") nextIndex = concepts.length - 1;
    const next = concepts[nextIndex];
    if (!next) return;
    selectConcept(next.id);
    queueMicrotask(() => {
      document.querySelector<HTMLButtonElement>(`[data-concept="${next.id}"]`)?.focus();
    });
  }

  return (
    <div className="initiative-lab">
      <header className="initiative-lab__header">
        <div className="initiative-lab__title-row">
          <div>
            <span className="initiative-lab__overline">Initiative work · exploration</span>
            <h1>Choose the shape of the work</h1>
          </div>
          <aside className="initiative-lab__sample" role="status">
            <strong>Sample data</strong>
            <span>
              {initiatives.length} initiatives · {workScenarios.length} efforts ·{" "}
              {workScenarios.reduce((total, work) => total + work.pullRequests.length, 0)} PRs
            </span>
          </aside>
        </div>
        <p className="initiative-lab__prompt">
          Compare the same delivery state three ways. Look for the view that makes your next decision feel obvious.
        </p>

        <div
          className="initiative-lab__switcher"
          role="tablist"
          aria-label="Initiative lab concepts"
          onKeyDown={moveConceptFocus}
        >
          {concepts.map((item, index) => (
            <button
              key={item.id}
              type="button"
              role="tab"
              id={`initiative-lab-tab-${item.id}`}
              aria-controls="initiative-lab-panel"
              aria-selected={item.id === concept}
              tabIndex={item.id === concept ? 0 : -1}
              data-concept={item.id}
              onClick={() => selectConcept(item.id)}
            >
              <span>0{index + 1}</span>
              <span>
                <small>{item.eyebrow}</small>
                <strong>{item.label}</strong>
              </span>
            </button>
          ))}
        </div>
      </header>

      <div
        id="initiative-lab-panel"
        className="initiative-lab__canvas"
        role="tabpanel"
        aria-labelledby={`initiative-lab-tab-${concept}`}
      >
        {activeConcept}
      </div>
    </div>
  );
}
