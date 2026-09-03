const STATUS_ENDPOINT = "https://mchostpack-urz.fly.dev/api/guest-status";
const grid = document.getElementById("pack-grid");
const count = document.getElementById("pack-count");
const button = document.getElementById("check-status");
const pill = document.getElementById("system-pill");
const note = document.getElementById("status-note");
let catalog = [];
let activeID = "";

function renderPacks() {
  if (!catalog.length) {
    grid.innerHTML = '<div class="empty">No packs are currently listed.</div>';
    return;
  }
  grid.replaceChildren(...catalog.map((pack, index) => {
    const card = document.createElement("article");
    card.className = "pack-card" + (pack.id === activeID ? " active" : "");
    card.dataset.index = String(index + 1).padStart(2, "0");
    const top = document.createElement("div");
    top.className = "pack-top";
    const provider = document.createElement("span");
    provider.className = "pack-provider";
    provider.textContent = `${pack.provider} · Java ${pack.java}`;
    const active = document.createElement("span");
    active.className = "active-tag";
    active.textContent = "ACTIVE";
    top.append(provider, active);
    const title = document.createElement("h3");
    title.textContent = pack.displayName;
    const addressRow = document.createElement("div");
    addressRow.className = "address-row";
    const address = document.createElement("div");
    address.className = "address";
    address.textContent = pack.hostname;
    const copy = document.createElement("button");
    copy.className = "copy";
    copy.type = "button";
    copy.textContent = "COPY";
    copy.addEventListener("click", async () => {
      await navigator.clipboard.writeText(pack.hostname);
      copy.textContent = "COPIED";
      setTimeout(() => { copy.textContent = "COPY"; }, 1400);
    });
    addressRow.append(address, copy);
    const body = document.createElement("div");
    body.append(title, addressRow);
    card.append(top, body);
    return card;
  }));
}

async function loadCatalog() {
  try {
    const response = await fetch("catalog.json", {cache: "no-store"});
    if (!response.ok) throw new Error("catalog unavailable");
    catalog = (await response.json()).packs || [];
    count.textContent = `${catalog.length} ${catalog.length === 1 ? "pack" : "packs"}`;
    renderPacks();
  } catch {
    count.textContent = "Unavailable";
    grid.innerHTML = '<div class="empty">The pack catalog could not be loaded.</div>';
  }
}

function describeStatus(status) {
  activeID = status.activeId || "";
  renderPacks();
  const phase = status.phase || "IDLE";
  pill.className = "system-pill";
  if (phase === "READY") {
    pill.querySelector("b").textContent = `${status.activeName || activeID} online`;
    note.textContent = "A Minecraft world is online now.";
  } else if (phase === "IDLE") {
    pill.querySelector("b").textContent = "Sleeping";
    note.textContent = "No world is running. Joining any listed address will wake it.";
  } else if (phase === "FAILED") {
    pill.classList.add("failed");
    pill.querySelector("b").textContent = "Needs attention";
    note.textContent = "The operator has been notified. No internal error details are public.";
  } else {
    pill.classList.add("busy");
    pill.querySelector("b").textContent = phase.toLowerCase();
    note.textContent = status.activeName ? `${status.activeName} is ${phase.toLowerCase()}.` : `The server is ${phase.toLowerCase()}.`;
  }
}

button.addEventListener("click", async () => {
  button.disabled = true;
  button.textContent = "Waking status…";
  note.textContent = "Fly may need a few seconds to start the lightweight router.";
  try {
    const response = await fetch(STATUS_ENDPOINT, {cache: "no-store"});
    if (!response.ok) throw new Error("status unavailable");
    describeStatus(await response.json());
  } catch {
    pill.className = "system-pill failed";
    pill.querySelector("b").textContent = "Status unavailable";
    note.textContent = "The guest page is online, but live Fly status could not be reached.";
  } finally {
    button.disabled = false;
    button.textContent = "Check live status";
  }
});

loadCatalog();
