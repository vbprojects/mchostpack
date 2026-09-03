const statusEndpoint = "https://mchostpack-urz.fly.dev/api/guest-status";
const list = document.getElementById("pack-list");
const count = document.getElementById("pack-count");
const statusText = document.getElementById("status");
const statusButton = document.getElementById("check-status");
let packs = [];
let activePack = "";

function renderPacks() {
  if (!packs.length) {
    list.innerHTML = '<div class="empty">No modpacks are currently listed.</div>';
    return;
  }
  list.replaceChildren(...packs.map(pack => {
    const row = document.createElement("article");
    row.className = "pack" + (pack.id === activePack ? " active" : "");
    const details = document.createElement("div");
    const heading = document.createElement("h3");
    heading.textContent = pack.displayName;
    if (pack.id === activePack) {
      const active = document.createElement("span");
      active.className = "active-label";
      active.textContent = "Active";
      heading.append(active);
    }
    const meta = document.createElement("div");
    meta.className = "meta";
    meta.textContent = `${pack.provider} · Java ${pack.java}`;
    details.append(heading, meta);
    if (pack.packUrl) {
      const packLink = document.createElement("a");
      packLink.className = "pack-link";
      packLink.href = pack.packUrl;
      packLink.target = "_blank";
      packLink.rel = "noopener noreferrer";
      packLink.textContent = "Pack page";
      details.append(packLink);
    }
    const addressRow = document.createElement("div");
    addressRow.className = "address-row";
    const address = document.createElement("span");
    address.className = "address";
    address.textContent = pack.hostname;
    const copy = document.createElement("button");
    copy.className = "copy";
    copy.type = "button";
    copy.textContent = "Copy";
    copy.addEventListener("click", async () => {
      await navigator.clipboard.writeText(pack.hostname);
      copy.textContent = "Copied";
      setTimeout(() => { copy.textContent = "Copy"; }, 1200);
    });
    addressRow.append(address, copy);
    row.append(details, addressRow);
    return row;
  }));
}

async function loadCatalog() {
  try {
    const response = await fetch("catalog.json", {cache: "no-store"});
    if (!response.ok) throw new Error("catalog unavailable");
    packs = (await response.json()).packs || [];
    count.textContent = `${packs.length} ${packs.length === 1 ? "modpack" : "modpacks"}`;
    renderPacks();
  } catch {
    count.textContent = "Unavailable";
    list.innerHTML = '<div class="empty">The modpack list could not be loaded.</div>';
  }
}

function showStatus(status) {
  activePack = status.activeId || "";
  renderPacks();
  statusText.className = "status";
  if (status.phase === "READY") {
    statusText.textContent = `${status.activeName || activePack} is online`;
    statusText.classList.add("ready");
  } else if (status.phase === "IDLE") {
    statusText.textContent = "Server is sleeping";
  } else if (status.phase === "FAILED") {
    statusText.textContent = "Server needs attention";
    statusText.classList.add("failed");
  } else {
    statusText.textContent = `Server is ${(status.phase || "starting").toLowerCase()}`;
    statusText.classList.add("busy");
  }
}

statusButton.addEventListener("click", async () => {
  statusButton.disabled = true;
  statusButton.textContent = "Checking…";
  try {
    const response = await fetch(statusEndpoint, {cache: "no-store"});
    if (!response.ok) throw new Error("status unavailable");
    showStatus(await response.json());
  } catch {
    statusText.textContent = "Status unavailable";
    statusText.className = "status failed";
  } finally {
    statusButton.disabled = false;
    statusButton.textContent = "Check status";
  }
});

loadCatalog();

const modrinthForm = document.getElementById("modrinth-form");
const modrinthVersion = document.getElementById("modrinth-version");
const modrinthOutput = document.getElementById("modrinth-output");
const modrinthError = document.getElementById("modrinth-error");
let modrinthProject = null;

function projectSlug(value) {
  const url = new URL(value);
  if (url.hostname !== "modrinth.com") throw new Error("Use a modrinth.com project URL.");
  const parts = url.pathname.split("/").filter(Boolean);
  if (parts.length < 2 || !["mod", "modpack", "plugin", "resourcepack", "shader"].includes(parts[0])) {
    throw new Error("Use a Modrinth project or modpack URL.");
  }
  return parts[1];
}

function renderModrinthConfig() {
  const selected = modrinthVersion.selectedOptions[0];
  if (!modrinthProject || !selected || !selected.value) {
    modrinthOutput.textContent = "";
    return;
  }
  modrinthOutput.textContent = `provider: modrinth\nproject_id: ${modrinthProject.id}\nversion_id: ${selected.value}`;
}

modrinthForm.addEventListener("submit", async event => {
  event.preventDefault();
  modrinthError.textContent = "";
  modrinthOutput.textContent = "Loading…";
  modrinthVersion.disabled = true;
  try {
    const slug = projectSlug(document.getElementById("modrinth-url").value.trim());
    const projectResponse = await fetch(`https://api.modrinth.com/v2/project/${encodeURIComponent(slug)}`);
    if (!projectResponse.ok) throw new Error("Modrinth project was not found.");
    modrinthProject = await projectResponse.json();
    const versionsResponse = await fetch(`https://api.modrinth.com/v2/project/${encodeURIComponent(modrinthProject.id)}/version`);
    if (!versionsResponse.ok) throw new Error("Could not load Modrinth versions.");
    const versions = await versionsResponse.json();
    modrinthVersion.replaceChildren(...versions.map(version => {
      const option = document.createElement("option");
      option.value = version.id;
      option.textContent = `${version.version_number} (${version.id})`;
      return option;
    }));
    modrinthVersion.disabled = versions.length === 0;
    if (!versions.length) throw new Error("This project has no published versions.");
    renderModrinthConfig();
  } catch (error) {
    modrinthProject = null;
    modrinthVersion.replaceChildren(new Option("Select a project first"));
    modrinthVersion.disabled = true;
    modrinthOutput.textContent = "";
    modrinthError.textContent = error.message || "Could not read Modrinth metadata.";
  }
});
modrinthVersion.addEventListener("change", renderModrinthConfig);
