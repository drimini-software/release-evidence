type EvidenceState = "observed" | "not_observed" | "unknown" | "not_applicable";
type Disposition = "open" | "assigned" | "accepted" | "not_applicable" | "resolved";
type AssessmentStatus = "not_started" | "in_progress" | "complete";
type DecisionStatus = "not_made" | "approved" | "blocked" | "needs_work";
type SaveState = "loading" | "clean" | "dirty" | "saving" | "saved" | "error";

interface Citation {
  path: string;
  line?: number;
}

interface Finding {
  id: string;
  category: string;
  title: string;
  state: EvidenceState;
  confidence: "high" | "medium" | "low";
  explanation: string;
  limitations: string;
  search_boundary: string[];
  citations: Citation[];
}

interface AssessmentItem {
  finding_id: string;
  disposition: Disposition;
  rationale?: string;
  owner?: string;
  target_date?: string;
}

interface Assessment {
  status: AssessmentStatus;
  context?: string;
  items?: AssessmentItem[];
  updated_at?: string;
}

interface Decision {
  status: DecisionStatus;
  rationale?: string;
  owner?: string;
  updated_at?: string;
}

interface Packet {
  schema_version: string;
  tool: { name: string; version: string; rule_set_version: string };
  repository: {
    name: string;
    revision?: string;
    revision_state: EvidenceState;
    working_tree_state: EvidenceState;
    working_tree_note: string;
  };
  scan: {
    generated_at: string;
    complete: boolean;
    entries_visited: number;
    files_considered: number;
  };
  findings: Finding[];
  diagnostics: Array<{ code: string; severity: string; path?: string; message: string }>;
  assessment: Assessment;
  decision: Decision;
  integrity: { digest: string; note: string };
}

interface ReviewUpdate {
  assessment: Assessment;
  decision: Decision;
}

const tokenValue = document.querySelector<HTMLMetaElement>('meta[name="review-token"]')?.content;
if (!tokenValue) {
  throw new Error("Review session token is missing.");
}
const token: string = tokenValue;

const summary = mustElement<HTMLElement>("#summary");
const findingsRoot = mustElement<HTMLElement>("#findings");
const reviewForm = mustElement<HTMLFormElement>("#review-form");
const assessmentStatus = mustElement<HTMLSelectElement>("#assessment-status");
const assessmentContext = mustElement<HTMLTextAreaElement>("#assessment-context");
const decisionStatus = mustElement<HTMLSelectElement>("#decision-status");
const decisionOwner = mustElement<HTMLInputElement>("#decision-owner");
const decisionRationale = mustElement<HTMLTextAreaElement>("#decision-rationale");
const saveButton = mustElement<HTMLButtonElement>("#save-review");
const saveStatus = mustElement<HTMLElement>("#save-status");
const diagnosticsRoot = mustElement<HTMLElement>("#diagnostics");

let packet: Packet;
let isDirty = false;
let isSaving = false;

void load();

reviewForm.addEventListener("input", markDirty);
reviewForm.addEventListener("change", markDirty);

reviewForm.addEventListener("submit", (event) => {
  event.preventDefault();
  void save();
});

window.addEventListener("beforeunload", (event) => {
  if (!isDirty) return;
  event.preventDefault();
  event.returnValue = "";
});

async function load(): Promise<void> {
  saveButton.disabled = true;
  setStatus("Loading local evidence…", false, "loading");
  try {
    packet = await request<Packet>("/api/packet", { method: "GET" });
    render();
    isDirty = false;
    saveButton.disabled = true;
    setStatus(lastSavedMessage(packet, false), false, "clean");
  } catch (error) {
    setStatus(errorMessage(error), true, "error");
    saveButton.disabled = true;
  }
}

function render(): void {
  renderSummary();
  renderDiagnostics();

  assessmentStatus.value = packet.assessment.status;
  assessmentContext.value = packet.assessment.context ?? "";
  decisionStatus.value = packet.decision.status;
  decisionOwner.value = packet.decision.owner ?? "";
  decisionRationale.value = packet.decision.rationale ?? "";

  const existing = new Map((packet.assessment.items ?? []).map((item) => [item.finding_id, item]));
  findingsRoot.replaceChildren();
  for (const finding of packet.findings) {
    findingsRoot.append(createFinding(finding, existing.get(finding.id)));
  }
}

function renderSummary(): void {
  const counts = new Map<EvidenceState, number>();
  for (const finding of packet.findings) {
    counts.set(finding.state, (counts.get(finding.state) ?? 0) + 1);
  }

  const cards = [
    ["Repository", packet.repository.name],
    ["Observed", String(counts.get("observed") ?? 0)],
    ["Not observed", String(counts.get("not_observed") ?? 0)],
    ["Unknown", String(counts.get("unknown") ?? 0)],
    ["Scan", packet.scan.complete ? "Complete" : "Incomplete"],
  ];

  summary.replaceChildren();
  for (const [label, value] of cards) {
    const card = element("div", "summary-card");
    card.append(element("span", "summary-card__label", label));
    card.append(element("strong", "summary-card__value", value));
    summary.append(card);
  }

  mustElement<HTMLElement>("#scan-meta").textContent =
    `${packet.scan.files_considered} files considered · rules ${packet.tool.rule_set_version} · ${packet.integrity.digest.slice(0, 12)}…`;
}

function renderDiagnostics(): void {
  diagnosticsRoot.replaceChildren();
  if (packet.diagnostics.length === 0) {
    diagnosticsRoot.hidden = true;
    return;
  }
  diagnosticsRoot.hidden = false;
  const heading = element("h2", "section-title", "Scan diagnostics");
  const list = element("ul", "diagnostic-list");
  for (const diagnostic of packet.diagnostics) {
    const item = element("li");
    const location = diagnostic.path ? ` (${diagnostic.path})` : "";
    item.textContent = `${diagnostic.severity}: ${diagnostic.message}${location}`;
    list.append(item);
  }
  diagnosticsRoot.append(heading, list);
}

function createFinding(finding: Finding, existing?: AssessmentItem): HTMLElement {
  const article = element("article", `finding finding--${finding.state}`);
  article.dataset.findingId = finding.id;

  const header = element("header", "finding__header");
  const headingGroup = element("div");
  headingGroup.append(
    element("span", "finding__category", finding.category),
    element("h3", "finding__title", finding.title),
  );
  header.append(headingGroup, element("span", `state state--${finding.state}`, stateLabel(finding.state)));

  const result = element("p", "finding__result", findingResult(finding));
  const evidence = element("div", "finding__evidence");
  evidence.append(element("h4", "finding__subheading", "Why the scanner returned this result"));
  if (finding.citations.length === 0) {
    evidence.append(element("p", "muted", "No supported repository citation was found for this check."));
  } else {
    const list = element("ul", "citation-list");
    for (const citation of finding.citations) {
      const item = element("li");
      const code = element("code", "citation", citation.line ? `${citation.path}:${citation.line}` : citation.path);
      item.append(code);
      list.append(item);
    }
    evidence.append(list);
  }

  const limit = element("details", "finding__limit");
  const boundaryHeading = element("h4", "finding__subheading", "Search boundary");
  const boundary = element("ul", "finding__scope");
  for (const item of finding.search_boundary) {
    boundary.append(element("li", "", item));
  }
  limit.append(
    element("summary", "", "Limits and search boundary"),
    element("p", "", finding.limitations),
    boundaryHeading,
    boundary,
  );

  const controls = element("fieldset", "assessment-controls");
  const legend = element("legend", "assessment-controls__legend", "Human assessment");
  controls.append(legend);

  const disposition = selectControl(
    "Disposition",
    "disposition",
    [
      ["open", "Open"],
      ["assigned", "Assigned"],
      ["accepted", "Risk accepted"],
      ["not_applicable", "Not applicable"],
      ["resolved", "Resolved"],
    ],
    existing?.disposition ?? "open",
    "Open = undecided; Assigned = an owner is named; Risk accepted = consciously accepted; Not applicable = irrelevant here; Resolved = addressed.",
    `finding-${finding.id.replaceAll(".", "-")}-disposition-hint`,
  );
  const owner = inputControl("Owner", "owner", "text", existing?.owner ?? "", 200);
  const targetDate = inputControl("Target date", "target-date", "date", existing?.target_date ?? "", 10);
  const rationale = textareaControl(
    "Rationale or next action",
    "rationale",
    existing?.rationale ?? "",
    4000,
    "Explain why this is acceptable, not applicable, or what must happen next.",
    `finding-${finding.id.replaceAll(".", "-")}-rationale-hint`,
  );
  controls.append(disposition, owner, targetDate, rationale);

  article.append(header, result, evidence, limit, controls);
  return article;
}

async function save(): Promise<void> {
  if (!isDirty || isSaving) return;
  isSaving = true;
  saveButton.disabled = true;
  saveButton.textContent = "Saving…";
  setStatus("Saving changes to this local packet…", false, "saving");
  try {
    const update: ReviewUpdate = {
      assessment: {
        status: assessmentStatus.value as AssessmentStatus,
        context: assessmentContext.value.trim(),
        items: collectAssessmentItems(),
      },
      decision: {
        status: decisionStatus.value as DecisionStatus,
        owner: decisionOwner.value.trim(),
        rationale: decisionRationale.value.trim(),
      },
    };
    packet = await request<Packet>("/api/review", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(update),
    });
    render();
    isDirty = false;
    setStatus(lastSavedMessage(packet, true), false, "saved");
  } catch (error) {
    setStatus(errorMessage(error), true, "error");
  } finally {
    isSaving = false;
    saveButton.textContent = "Save changes";
    saveButton.disabled = !isDirty;
  }
}

function collectAssessmentItems(): AssessmentItem[] {
  const items: AssessmentItem[] = [];
  for (const article of findingsRoot.querySelectorAll<HTMLElement>("[data-finding-id]")) {
    const findingId = article.dataset.findingId;
    if (!findingId) continue;
    const disposition = field<HTMLSelectElement>(article, "disposition").value as Disposition;
    const rationale = field<HTMLTextAreaElement>(article, "rationale").value.trim();
    const owner = field<HTMLInputElement>(article, "owner").value.trim();
    const targetDate = field<HTMLInputElement>(article, "target-date").value;
    items.push({
      finding_id: findingId,
      disposition,
      ...(rationale ? { rationale } : {}),
      ...(owner ? { owner } : {}),
      ...(targetDate ? { target_date: targetDate } : {}),
    });
  }
  return items;
}

async function request<T>(url: string, init: RequestInit): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("X-Release-Evidence-Token", token);
  const response = await fetch(url, { ...init, headers, credentials: "same-origin" });
  if (!response.ok) {
    const body = (await response.json().catch(() => ({ error: `Request failed (${response.status}).` }))) as { error?: string };
    throw new Error(body.error ?? `Request failed (${response.status}).`);
  }
  return (await response.json()) as T;
}

function selectControl(
  label: string,
  name: string,
  options: Array<[string, string]>,
  value: string,
  hint = "",
  hintId = "",
): HTMLElement {
  const wrapper = element("div", "control");
  const labelElement = element("label", "control__label", label);
  const select = document.createElement("select");
  const controlId = hintId.endsWith("-hint") ? hintId.slice(0, -5) : `${name}-control`;
  select.id = controlId;
  labelElement.htmlFor = controlId;
  select.dataset.field = name;
  for (const [optionValue, text] of options) {
    const option = document.createElement("option");
    option.value = optionValue;
    option.textContent = text;
    option.selected = optionValue === value;
    select.append(option);
  }
  if (hint && hintId) {
    select.setAttribute("aria-describedby", hintId);
  }
  wrapper.append(labelElement, select);
  if (hint && hintId) {
    wrapper.append(element("span", "control__hint", hint, hintId));
  }
  return wrapper;
}

function inputControl(label: string, name: string, type: string, value: string, maxLength: number): HTMLLabelElement {
  const wrapper = element("label", "control");
  wrapper.append(element("span", "control__label", label));
  const input = document.createElement("input");
  input.type = type;
  input.value = value;
  input.maxLength = maxLength;
  input.dataset.field = name;
  wrapper.append(input);
  return wrapper;
}

function textareaControl(label: string, name: string, value: string, maxLength: number, hint: string, hintId: string): HTMLLabelElement {
  const wrapper = element("label", "control control--wide");
  wrapper.append(element("span", "control__label", label));
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.maxLength = maxLength;
  textarea.rows = 3;
  textarea.dataset.field = name;
  textarea.setAttribute("aria-describedby", hintId);
  wrapper.append(textarea, element("span", "control__hint", hint, hintId));
  return wrapper;
}

function field<T extends HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>(root: HTMLElement, name: string): T {
  const result = root.querySelector<T>(`[data-field="${name}"]`);
  if (!result) throw new Error(`Missing review field ${name}.`);
  return result;
}

function stateLabel(state: EvidenceState): string {
  switch (state) {
    case "observed": return "Observed";
    case "not_observed": return "Not observed";
    case "unknown": return "Unknown";
    case "not_applicable": return "Not applicable";
  }
}

function findingResult(finding: Finding): string {
  switch (finding.state) {
    case "observed":
      return `Scanner result: supported repository evidence for “${finding.title}” was found.`;
    case "not_observed":
      return `Scanner result: supported repository evidence for “${finding.title}” was not found within the stated search boundary.`;
    case "unknown":
      return `Scanner result: “${finding.title}” could not be evaluated reliably.`;
    case "not_applicable":
      return `Scanner result: “${finding.title}” is marked not applicable.`;
  }
}

function markDirty(): void {
  if (isSaving) return;
  isDirty = true;
  saveButton.disabled = false;
  setStatus("Unsaved changes in this packet.", false, "dirty");
}

function lastSavedMessage(current: Packet, justSaved: boolean): string {
  const value = current.assessment.updated_at ?? current.decision.updated_at;
  if (!value) return "No unsaved changes. This packet has not been reviewed yet.";
  const instant = new Date(value);
  if (Number.isNaN(instant.getTime())) return justSaved ? "Saved locally. No unsaved changes." : "No unsaved changes.";
  const time = instant.toISOString().slice(11, 19);
  return `${justSaved ? "Saved locally" : "No unsaved changes. Last saved"} at ${time} UTC.`;
}

function setStatus(message: string, isError: boolean, state: SaveState): void {
  saveStatus.textContent = message;
  saveStatus.dataset.error = String(isError);
  saveStatus.dataset.state = state;
}

function errorMessage(error: unknown): string {
  if (error instanceof TypeError) {
    return "The local review session is no longer reachable. Restart the review command; unsaved changes remain in this tab.";
  }
  return error instanceof Error ? error.message : "An unexpected local review error occurred.";
}

function mustElement<T extends Element>(selector: string): T {
  const result = document.querySelector<T>(selector);
  if (!result) throw new Error(`Missing interface element ${selector}.`);
  return result;
}

function element<K extends keyof HTMLElementTagNameMap>(tag: K, className = "", text = "", id = ""): HTMLElementTagNameMap[K] {
  const result = document.createElement(tag);
  if (className) result.className = className;
  if (text) result.textContent = text;
  if (id) result.id = id;
  return result;
}
