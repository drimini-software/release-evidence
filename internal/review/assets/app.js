"use strict";
const tokenValue = document.querySelector('meta[name="review-token"]')?.content;
if (!tokenValue) {
    throw new Error("Review session token is missing.");
}
const token = tokenValue;
const summary = mustElement("#summary");
const findingsRoot = mustElement("#findings");
const reviewForm = mustElement("#review-form");
const assessmentStatus = mustElement("#assessment-status");
const assessmentContext = mustElement("#assessment-context");
const decisionStatus = mustElement("#decision-status");
const decisionOwner = mustElement("#decision-owner");
const decisionRationale = mustElement("#decision-rationale");
const saveButton = mustElement("#save-review");
const saveStatus = mustElement("#save-status");
const diagnosticsRoot = mustElement("#diagnostics");
let packet;
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
    if (!isDirty)
        return;
    event.preventDefault();
    event.returnValue = "";
});
async function load() {
    saveButton.disabled = true;
    setStatus("Loading local evidence…", false, "loading");
    try {
        packet = await request("/api/packet", { method: "GET" });
        render();
        isDirty = false;
        saveButton.disabled = true;
        setStatus(lastSavedMessage(packet, false), false, "clean");
    }
    catch (error) {
        setStatus(errorMessage(error), true, "error");
        saveButton.disabled = true;
    }
}
function render() {
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
function renderSummary() {
    const counts = new Map();
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
    mustElement("#scan-meta").textContent =
        `${packet.scan.files_considered} files considered · rules ${packet.tool.rule_set_version} · ${packet.integrity.digest.slice(0, 12)}…`;
}
function renderDiagnostics() {
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
function createFinding(finding, existing) {
    const article = element("article", `finding finding--${finding.state}`);
    article.dataset.findingId = finding.id;
    const header = element("header", "finding__header");
    const headingGroup = element("div");
    headingGroup.append(element("span", "finding__category", finding.category), element("h3", "finding__title", finding.title));
    header.append(headingGroup, element("span", `state state--${finding.state}`, stateLabel(finding.state)));
    const result = element("p", "finding__result", findingResult(finding));
    const evidence = element("div", "finding__evidence");
    evidence.append(element("h4", "finding__subheading", "Why the scanner returned this result"));
    if (finding.citations.length === 0) {
        evidence.append(element("p", "muted", "No supported repository citation was found for this check."));
    }
    else {
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
    limit.append(element("summary", "", "Limits and search boundary"), element("p", "", finding.limitations), boundaryHeading, boundary);
    const controls = element("fieldset", "assessment-controls");
    const legend = element("legend", "assessment-controls__legend", "Human assessment");
    controls.append(legend);
    const disposition = selectControl("Disposition", "disposition", [
        ["open", "Open"],
        ["assigned", "Assigned"],
        ["accepted", "Risk accepted"],
        ["not_applicable", "Not applicable"],
        ["resolved", "Resolved"],
    ], existing?.disposition ?? "open", "Open = undecided; Assigned = an owner is named; Risk accepted = consciously accepted; Not applicable = irrelevant here; Resolved = addressed.", `finding-${finding.id.replaceAll(".", "-")}-disposition-hint`);
    const owner = inputControl("Owner", "owner", "text", existing?.owner ?? "", 200);
    const targetDate = inputControl("Target date", "target-date", "date", existing?.target_date ?? "", 10);
    const rationale = textareaControl("Rationale or next action", "rationale", existing?.rationale ?? "", 4000, "Explain why this is acceptable, not applicable, or what must happen next.", `finding-${finding.id.replaceAll(".", "-")}-rationale-hint`);
    controls.append(disposition, owner, targetDate, rationale);
    article.append(header, result, evidence, limit, controls);
    return article;
}
async function save() {
    if (!isDirty || isSaving)
        return;
    isSaving = true;
    saveButton.disabled = true;
    saveButton.textContent = "Saving…";
    setStatus("Saving changes to this local packet…", false, "saving");
    try {
        const update = {
            assessment: {
                status: assessmentStatus.value,
                context: assessmentContext.value.trim(),
                items: collectAssessmentItems(),
            },
            decision: {
                status: decisionStatus.value,
                owner: decisionOwner.value.trim(),
                rationale: decisionRationale.value.trim(),
            },
        };
        packet = await request("/api/review", {
            method: "PUT",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(update),
        });
        render();
        isDirty = false;
        setStatus(lastSavedMessage(packet, true), false, "saved");
    }
    catch (error) {
        setStatus(errorMessage(error), true, "error");
    }
    finally {
        isSaving = false;
        saveButton.textContent = "Save changes";
        saveButton.disabled = !isDirty;
    }
}
function collectAssessmentItems() {
    const items = [];
    for (const article of findingsRoot.querySelectorAll("[data-finding-id]")) {
        const findingId = article.dataset.findingId;
        if (!findingId)
            continue;
        const disposition = field(article, "disposition").value;
        const rationale = field(article, "rationale").value.trim();
        const owner = field(article, "owner").value.trim();
        const targetDate = field(article, "target-date").value;
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
async function request(url, init) {
    const headers = new Headers(init.headers);
    headers.set("X-Release-Evidence-Token", token);
    const response = await fetch(url, { ...init, headers, credentials: "same-origin" });
    if (!response.ok) {
        const body = (await response.json().catch(() => ({ error: `Request failed (${response.status}).` })));
        throw new Error(body.error ?? `Request failed (${response.status}).`);
    }
    return (await response.json());
}
function selectControl(label, name, options, value, hint = "", hintId = "") {
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
function inputControl(label, name, type, value, maxLength) {
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
function textareaControl(label, name, value, maxLength, hint, hintId) {
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
function field(root, name) {
    const result = root.querySelector(`[data-field="${name}"]`);
    if (!result)
        throw new Error(`Missing review field ${name}.`);
    return result;
}
function stateLabel(state) {
    switch (state) {
        case "observed": return "Observed";
        case "not_observed": return "Not observed";
        case "unknown": return "Unknown";
        case "not_applicable": return "Not applicable";
    }
}
function findingResult(finding) {
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
function markDirty() {
    if (isSaving)
        return;
    isDirty = true;
    saveButton.disabled = false;
    setStatus("Unsaved changes in this packet.", false, "dirty");
}
function lastSavedMessage(current, justSaved) {
    const value = current.assessment.updated_at ?? current.decision.updated_at;
    if (!value)
        return "No unsaved changes. This packet has not been reviewed yet.";
    const instant = new Date(value);
    if (Number.isNaN(instant.getTime()))
        return justSaved ? "Saved locally. No unsaved changes." : "No unsaved changes.";
    const time = instant.toISOString().slice(11, 19);
    return `${justSaved ? "Saved locally" : "No unsaved changes. Last saved"} at ${time} UTC.`;
}
function setStatus(message, isError, state) {
    saveStatus.textContent = message;
    saveStatus.dataset.error = String(isError);
    saveStatus.dataset.state = state;
}
function errorMessage(error) {
    if (error instanceof TypeError) {
        return "The local review session is no longer reachable. Restart the review command; unsaved changes remain in this tab.";
    }
    return error instanceof Error ? error.message : "An unexpected local review error occurred.";
}
function mustElement(selector) {
    const result = document.querySelector(selector);
    if (!result)
        throw new Error(`Missing interface element ${selector}.`);
    return result;
}
function element(tag, className = "", text = "", id = "") {
    const result = document.createElement(tag);
    if (className)
        result.className = className;
    if (text)
        result.textContent = text;
    if (id)
        result.id = id;
    return result;
}
