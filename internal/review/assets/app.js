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
const exportControls = mustElement("#export-controls");
const exportStatus = mustElement("#export-status");
const exportOptions = {
    html: { button: mustElement("#export-html"), filename: "release-evidence-report.html", label: "HTML report" },
    markdown: { button: mustElement("#export-markdown"), filename: "release-evidence-report.md", label: "Markdown report" },
    json: { button: mustElement("#export-json"), filename: "release-evidence-packet.json", label: "JSON packet" },
};
const comparisonControls = mustElement("#comparison-controls");
const comparisonStatus = mustElement("#comparison-status");
const earlierPacketInput = mustElement("#comparison-packet");
const comparisonOptions = {
    html: { button: mustElement("#compare-html"), filename: "release-evidence-comparison.html", label: "HTML comparison" },
    markdown: { button: mustElement("#compare-markdown"), filename: "release-evidence-comparison.md", label: "Markdown comparison" },
    json: { button: mustElement("#compare-json"), filename: "release-evidence-comparison.json", label: "JSON comparison" },
};
const maxEarlierPacketBytes = 4 * 1024 * 1024;
let packet;
let packetVersion = "";
let hasSaveConflict = false;
let isLoaded = false;
let isDirty = false;
let isSaving = false;
let isExporting = false;
let isComparing = false;
let earlierPacket = null;
void load();
reviewForm.addEventListener("input", markDirty);
reviewForm.addEventListener("change", markDirty);
reviewForm.addEventListener("submit", (event) => {
    event.preventDefault();
    void save();
});
for (const format of ["html", "markdown", "json"]) {
    exportOptions[format].button.addEventListener("click", () => { void download(format); });
    comparisonOptions[format].button.addEventListener("click", () => { void download(format, true); });
}
earlierPacketInput.addEventListener("change", selectEarlierPacket);
window.addEventListener("beforeunload", (event) => {
    if (!isDirty)
        return;
    event.preventDefault();
    event.returnValue = "";
});
async function load() {
    refreshActions();
    setStatus("Loading local evidence…", false, "loading");
    try {
        packet = await requestPacket("/api/packet", { method: "GET" });
        render();
        isLoaded = true;
        isDirty = false;
        setStatus(lastSavedMessage(packet, false), false, "clean");
        setExportStatus("Ready to download the current packet.", false);
    }
    catch (error) {
        setStatus(errorMessage(error), true, "error");
        setExportStatus("Downloads are unavailable because the packet could not be loaded.", true);
    }
    finally {
        updateComparisonHint();
        refreshActions();
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
    if (!isLoaded || !isDirty || hasSaveConflict || isSaving || isExporting || isComparing)
        return;
    isSaving = true;
    refreshActions();
    saveButton.textContent = "Saving…";
    setStatus("Saving changes to this local packet…", false, "saving");
    setExportStatus("Downloads will be available after saving finishes.", false);
    updateComparisonHint();
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
        packet = await requestPacket("/api/review", {
            method: "PUT",
            headers: { "Content-Type": "application/json", "If-Match": packetVersion },
            body: JSON.stringify(update),
        });
        render();
        isDirty = false;
        setStatus(lastSavedMessage(packet, true), false, "saved");
        setExportStatus("Ready to download the current packet.", false);
    }
    catch (error) {
        hasSaveConflict = error instanceof RequestError && error.status === 412;
        setStatus(errorMessage(error), true, "error");
        setExportStatus("Save changes before downloading. Unsaved edits are excluded from exports.", false);
    }
    finally {
        isSaving = false;
        saveButton.textContent = "Save changes";
        updateComparisonHint();
        refreshActions();
        if (hasSaveConflict) {
            saveStatus.tabIndex = -1;
            saveStatus.focus();
        }
    }
}
async function download(format, compare = false) {
    if (!isLoaded || isDirty || isSaving || isExporting || isComparing)
        return;
    const previous = earlierPacket;
    if (compare && !previous)
        return;
    const option = (compare ? comparisonOptions : exportOptions)[format];
    const setDownloadStatus = compare ? setComparisonStatus : setExportStatus;
    const restoreFocus = document.activeElement === option.button;
    if (compare)
        isComparing = true;
    else
        isExporting = true;
    refreshActions();
    setDownloadStatus(`Preparing ${option.label}…`, false);
    try {
        const response = await requestResponse(compare ? `/api/compare?format=${format}` : "/api/export", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: compare ? previous : JSON.stringify({ format }),
        });
        const url = URL.createObjectURL(await response.blob());
        try {
            const link = document.createElement("a");
            link.href = url;
            link.download = option.filename;
            link.hidden = true;
            document.body.append(link);
            link.click();
            link.remove();
        }
        finally {
            window.setTimeout(() => URL.revokeObjectURL(url), 1000);
        }
        setDownloadStatus(`${option.label} download started. Check your browser's downloads.`, false);
    }
    catch (error) {
        setDownloadStatus(`Download failed. ${errorMessage(error)}`, true);
    }
    finally {
        if (compare)
            isComparing = false;
        else
            isExporting = false;
        refreshActions();
        if (restoreFocus && document.activeElement === document.body)
            option.button.focus({ preventScroll: true });
    }
}
function refreshActions() {
    const busy = isSaving || isExporting || isComparing;
    saveButton.disabled = !isLoaded || !isDirty || hasSaveConflict || busy;
    reviewForm.setAttribute("aria-busy", String(busy));
    for (const control of reviewForm.querySelectorAll("input, select, textarea")) {
        control.disabled = !isLoaded || busy;
    }
    exportControls.setAttribute("aria-busy", String(isExporting));
    for (const option of Object.values(exportOptions)) {
        option.button.disabled = !isLoaded || isDirty || busy;
    }
    comparisonControls.setAttribute("aria-busy", String(isComparing));
    earlierPacketInput.disabled = !isLoaded || busy;
    for (const option of Object.values(comparisonOptions)) {
        option.button.disabled = !isLoaded || isDirty || busy || !earlierPacket;
    }
}
function setExportStatus(message, isError) {
    exportStatus.textContent = message;
    exportStatus.dataset.error = String(isError);
}
function selectEarlierPacket() {
    earlierPacket = earlierPacketInput.files?.[0] ?? null;
    if (earlierPacket && (earlierPacket.size === 0 || earlierPacket.size > maxEarlierPacketBytes)) {
        const message = earlierPacket.size === 0 ? "The earlier packet is empty. Choose a saved JSON packet." : "The earlier packet exceeds the 4 MiB limit. Choose a smaller saved packet.";
        earlierPacket = null;
        earlierPacketInput.value = "";
        setComparisonStatus(message, true);
    }
    else {
        updateComparisonHint();
    }
    refreshActions();
}
function updateComparisonHint() {
    if (!isLoaded) {
        setComparisonStatus("Comparisons are unavailable until the current packet loads.", false);
    }
    else if (isSaving) {
        setComparisonStatus("Comparisons will be available after saving finishes.", false);
    }
    else if (isDirty) {
        setComparisonStatus("Save changes before comparing. Unsaved edits are excluded from comparisons.", false);
    }
    else if (earlierPacket) {
        setComparisonStatus(`Earlier packet selected: ${earlierPacket.name}. Choose a comparison format.`, false);
    }
    else {
        setComparisonStatus("Choose an earlier saved JSON packet to compare with the current review.", false);
    }
}
function setComparisonStatus(message, isError) {
    comparisonStatus.textContent = message;
    comparisonStatus.dataset.error = String(isError);
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
async function requestPacket(url, init) {
    const response = await requestResponse(url, init);
    const result = (await response.json());
    const version = response.headers.get("ETag");
    if (!version || !/^"[^"]+"$/.test(version)) {
        throw new Error("The review version is unavailable. Keep a copy of your edits, then reload the page.");
    }
    packetVersion = version;
    return result;
}
class RequestError extends Error {
    status;
    constructor(message, status) {
        super(message);
        this.status = status;
    }
}
async function requestResponse(url, init) {
    const headers = new Headers(init.headers);
    headers.set("X-Release-Evidence-Token", token);
    const response = await fetch(url, { ...init, headers, credentials: "same-origin" });
    if (!response.ok) {
        const body = (await response.json().catch(() => ({ error: `Request failed (${response.status}).` })));
        throw new RequestError(body.error ?? `Request failed (${response.status}).`, response.status);
    }
    return response;
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
    if (!isLoaded || isSaving || isExporting || isComparing)
        return;
    isDirty = true;
    refreshActions();
    if (!hasSaveConflict)
        setStatus("Unsaved changes in this packet.", false, "dirty");
    setExportStatus("Save changes before downloading. Unsaved edits are excluded from exports.", false);
    updateComparisonHint();
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
