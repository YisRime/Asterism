"use strict";

const active = new Set(), cache = new Map();
let offset = 0, total = 0, queryText = "", sorting = "score", selectedKey = "",
  busy = false, again = false, counted = "", countMark = 0;

const query = (selector) => document.querySelector(selector);
const escape = (value) => String(value ?? "").replace(/[&<>"]/g,
  (character) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[character]));
const days = (count) => count == null || count < 0 ? ""
  : count < 1 ? "今天" : count < 30 ? count + " 天前"
  : count < 365 ? Math.round(count / 30) + " 月前" : (count / 365).toFixed(1) + " 年前";
const signed = (value) => value == null ? ""
  : value === 0 ? "±0" : (value > 0 ? "+" : "-") + Math.abs(value).toFixed(2);
const format = (value) => {
  if (!value) return "";
  if (value >= 1e6) return (value / 1e6).toFixed(1).replace(/\.0$/, "") + "M";
  if (value >= 1e4) return Math.round(value / 1e3) + "k";
  return Number(value).toLocaleString("en-US");
};
const highlight = (text, search) => {
  if (!search) return escape(text);
  const index = text.toLowerCase().indexOf(search);
  if (index < 0) return escape(text);
  const word = (character) => /[A-Za-z0-9]/.test(character);
  let start = index, end = index + search.length;
  while (start > 0 && word(text[start - 1])) start--;
  while (end < text.length && word(text[end])) end++;
  return escape(text.slice(0, start)) + "<mark>" + escape(text.slice(start, end)) +
    "</mark>" + escape(text.slice(end));
};

async function fetchJSON(url) {
  const response = await fetch(url);
  if (!response.ok) throw new Error("HTTP " + response.status);
  return response.json();
}

function fetchInfo(key) {
  if (!cache.has(key)) {
    cache.set(key, fetchJSON("/api/info?key=" + encodeURIComponent(key))
      .catch((error) => {
        cache.delete(key);
        throw error;
      }));
  }
  return cache.get(key);
}

function listURL() {
  const parameters = new URLSearchParams();
  if (queryText) parameters.set("q", queryText);
  if (active.size) parameters.set("platforms", [...active].join(","));
  parameters.set("sort", sorting);
  parameters.set("limit", 100);
  parameters.set("offset", offset);
  return "/api/list?" + parameters.toString();
}

function countURL() {
  const parameters = new URLSearchParams();
  if (queryText) parameters.set("q", queryText);
  if (active.size) parameters.set("platforms", [...active].join(","));
  return "/api/count?" + parameters.toString();
}

const queryKey = () => queryText + "|" + [...active].join(",");

function paintPager() {
  query(".pager").hidden = total === 0;
  query("#page").textContent = total
    ? offset / 100 + 1 + " / " + Math.ceil(total / 100) : "";
  query("#prev").disabled = offset === 0;
  query("#next").disabled = offset + 100 >= total;
}

function fillRow(item) {
  const search = queryText.toLowerCase();
  const license = item.license || "";
  const upper = license.toUpperCase();
  const kind = /PROPRIETARY/.test(upper) ? "license-proprietary"
    : /^((L|A)?GPL|GPL-)/.test(upper) ? "license-copyleft"
    : /^(MIT|BSD|APACHE|ZLIB|ISC|MPL)/.test(upper) ? "license-permissive" : "";
  const node = query("#row-template").content.firstElementChild.cloneNode(true);
  node.dataset.key = item.key;
  const name = node.querySelector(".name");
  name.innerHTML = highlight(item.name, search);
  const sub = node.querySelector(".sub");
  const subParts = [item.version ? `<span>${escape(item.version)}</span>` : "",
    item.age != null ? `<span>${days(item.age)}</span>` : ""].filter(Boolean);
  sub.hidden = !subParts.length;
  if (subParts.length) sub.innerHTML = subParts.join(" · ");
  const score = node.querySelector(".score");
  score.className = "score " + (item.score >= 8 ? "score-high" : item.score >= 6 ? "score-mid" : "");
  score.textContent = item.score.toFixed(2);
  const trend = node.querySelector(".trend");
  trend.hidden = !(item.trend != null && Math.abs(item.trend) >= 0.05);
  if (!trend.hidden) {
    trend.classList.add(item.trend > 0 ? "up" : "down");
    trend.textContent = signed(item.trend);
  }
  const dl = node.querySelector(".dl");
  dl.hidden = !item.download;
  if (item.download) dl.textContent = format(item.download);
  const seen = new Set();
  const links = [
    item.homepage && ["homepage", item.homepage],
    item.repository && ["repository", "https://" + item.repository],
    ...Object.entries(item.link || {}).filter(([kind]) => ["changelog", "bugtracker", "documentation"].includes(kind)),
  ].filter(Boolean)
    .filter(([, url]) => url && !seen.has(url) && seen.add(url))
    .map(([kind, url]) =>
      `<a class="lnk ${kind}" href="${escape(url)}" target="_blank" rel="noopener" aria-label="${kind}"></a>`).join("");
  const desc = node.querySelector(".desc");
  const text = desc.querySelector(".text");
  text.hidden = !item.summary;
  if (item.summary) text.innerHTML = highlight(item.summary, search);
  desc.hidden = !item.summary && !item.download;
  const meta = node.querySelector(".meta");
  meta.innerHTML = item.platform.map((platform) =>
    `<span class="plat ${platform}">${platform}</span>`).join("") +
    (links ? `<span class="links">${links}</span>` : "") +
    (license ? `<span class="tag ${kind} push">${escape(license)}</span>` : "");
  meta.hidden = !meta.innerHTML;
  return node;
}

async function paint() {
  if (busy) {
    again = true;
    return;
  }
  busy = true;
  query("#prev").disabled = query("#next").disabled = true;
  try {
    const data = await fetchJSON(listURL());
    query("#ledger").replaceChildren(...data.items.map(fillRow));
    closeDetail();
    query("#empty").hidden = data.items.length > 0;
    query(".list").scrollTop = 0;
  } catch (error) {
    busy = false;
    query("#empty").hidden = false;
    return;
  }
  busy = false;
  if (again) {
    again = false;
    paint();
    return;
  }
  const key = queryKey();
  if (key !== counted) {
    counted = key;
    total = 0;
    query(".pager").hidden = true;
    const mark = ++countMark;
    fetchJSON(countURL()).then((data) => {
      if (countMark !== mark) return;
      total = data.total;
      paintPager();
    }).catch(() => {});
  } else {
    paintPager();
  }
}

function apply() {
  offset = 0;
  return paint();
}

function renderStats() {
  const paint = (stats) => {
    query("#stat-software").textContent = format(stats.software);
    query("#stat-package").textContent = format(stats.package);
    query("#stat-multi").textContent = format(stats.multi);
    query("#stat-repo").textContent = format(stats.repo);
    query("#stat-download").textContent = format(stats.download);
    query("#stat-star").textContent = stats.with_star && stats.software
      ? Math.round(stats.with_star * 100 / stats.software) + "%" : "";
    query("#gen").textContent = stats.build || "";
    const counts = stats.platform || {};
    const platforms = [...document.querySelectorAll("#platforms li")];
    const max = Math.max(1, ...platforms.map((item) => counts[item.className] || 0));
    platforms.forEach((item) => {
      const count = counts[item.className] || 0;
      item.style.setProperty("--w", Math.round(count * 100 / max) + "%");
      item.querySelector("b").textContent = format(count);
    });
    if (!selectedKey) paintInstall(null);
  };
  paint({});
  return fetchJSON("/api/meta").then(paint).catch(() => {});
}

function detail(panel, entry) {
  const metric = (name, value, show) => {
    const box = panel.querySelector('[data-metric="' + name + '"]');
    box.hidden = !show;
    box.querySelector("b").textContent = show ? value : "";
  };
  const date = (value) => value ? value.slice(0, 10) : "";
  const facts = [
    ["star", format(entry.star), entry.star != null],
    ["size", entry.size >= 1 << 30 ? (entry.size / (1 << 30)).toFixed(1) + " GB"
      : entry.size >= 1 << 20 ? (entry.size / (1 << 20)).toFixed(1) + " MB"
      : entry.size >= 1 << 10 ? Math.round(entry.size / (1 << 10)) + " KB"
      : entry.size + " B", entry.size > 0],
    ["change", date(entry.change), Boolean(entry.change)],
    ["window", entry.window, Boolean(entry.window)],
  ];
  facts.forEach(([name, value, show]) => metric(name, value, show));
  panel.querySelector(".kv").hidden = !facts.some(([, , show]) => show);

  const description = panel.querySelector(".desc");
  const text = entry.description === entry.summary ? "" : entry.description || "";
  description.hidden = !text;
  description.innerHTML = text ? escape(text).replace(/\n/g, "<br>") : "";

  const [meta] = panel.querySelectorAll(".meta");
  const [auth, maint, depend, lab] = meta.querySelectorAll("span");
  const join = (values) => values.join(" · ");
  const deps = entry.depend.filter((item) => !item.startsWith("so:"));
  const tags = [...new Set(Object.values(entry.label || {}).flat())];
  auth.hidden = !entry.author;
  if (entry.author) auth.querySelector("b").textContent = entry.author;
  maint.hidden = !entry.maintainer.length;
  maint.querySelector("b").textContent = join(entry.maintainer);
  depend.hidden = !deps.length;
  depend.querySelector("b").textContent = join(deps);
  lab.hidden = !tags.length;
  if (tags.length) lab.querySelector("b").textContent = join(tags);
  meta.hidden = [auth, maint, depend, lab].every((segment) => segment.hidden);

  const signals = {
    appinn: ["AppInn", "media"], itsfoss: ["It's FOSS", "media"], omg_ubuntu: ["OMG! Ubuntu", "media"],
    phoronix: ["Phoronix", "tech"], aur: ["AUR Updates", "aur"], releases: ["GitHub Releases", "releases"],
    lobsters: ["Lobsters", "forum"], hn_show: ["Hacker News", "forum"], reddit_sh: ["r/selfhosted", "forum"],
  };
  const events = entry.event.slice(0, 5).map((event) => {
    const [name, kind] = signals[event.source] || [event.source, "forum"];
    const moment = Date.parse(event.date);
    return `<li>
      <span class="tag sig-${kind}">${escape(name)}</span>
      ${event.title ? `<a href="${escape(event.url)}" target="_blank" rel="noopener">${escape(event.title)}</a>` : ""}
      <time>${Number.isNaN(moment) ? "" : days(Math.max(0, Math.floor((Date.now() - moment) / 864e5)))}</time>
      ${event.summary ? `<p>${escape(event.summary)}</p>` : ""}
    </li>`;
  }).join("");
  const list = panel.querySelector(".signals");
  list.innerHTML = events;
  list.hidden = list.previousElementSibling.hidden = !events;
}

function paintInstall(entry) {
  const installs = entry ? entry.install : [];
  document.querySelectorAll("#install .cmd").forEach((row) => {
    const install = installs.find((item) => item.source === row.dataset.source);
    const code = row.querySelector("code");
    if (!row.dataset.tpl) row.dataset.tpl = code.textContent;
    const command = install && install.command || "";
    const url = install && install.url || "";
    code.textContent = command || url || (install && install.id) || row.dataset.tpl;
    code.title = code.textContent;
    row.classList.toggle("on", Boolean(command || url));
    const copy = row.querySelector(".copy");
    if (copy) {
      copy.hidden = !command;
      copy.classList.remove("done");
      if (command) copy.dataset.copy = command; else delete copy.dataset.copy;
    }
    const open = row.querySelector(".open");
    if (open) {
      open.hidden = !url;
      if (url) open.href = url; else open.removeAttribute("href");
    }
  });
}

function closeDetail() {
  document.querySelector(".row.open")?.classList.remove("open");
  document.querySelector(".detail")?.remove();
  selectedKey = "";
  paintInstall(null);
}

async function selectSoftware(key, element) {
  if (selectedKey === key) {
    closeDetail();
    return;
  }
  closeDetail();
  selectedKey = key;
  element.classList.add("open");
  const panel = document.createElement("div");
  panel.className = "detail";
  panel.append(query("#detail-template").content.cloneNode(true));
  element.after(panel);
  panel.scrollIntoView({ block: "nearest" });
  try {
    const entity = await fetchInfo(key);
    if (selectedKey !== key) return;
    detail(panel, entity);
    paintInstall(entity);
  } catch (error) {
    if (selectedKey === key) panel.querySelector(".hint").hidden = false;
  }
}

document.addEventListener("click", (event) => {
  const target = event.target;
  const copy = target.closest("[data-copy]");
  if (copy) {
    event.stopPropagation();
    navigator.clipboard.writeText(copy.dataset.copy);
    copy.classList.add("done");
    setTimeout(() => copy.classList.remove("done"), 900);
    return;
  }
  if (target.closest("#prev")) {
    offset = Math.max(0, offset - 100);
    paint();
    return;
  }
  if (target.closest("#next")) {
    offset += 100;
    paint();
    return;
  }
  const platform = target.closest(".tg");
  if (platform) {
    if (active.has(platform.dataset.p)) active.delete(platform.dataset.p);
    else active.add(platform.dataset.p);
    platform.setAttribute("aria-pressed", active.has(platform.dataset.p));
    apply();
    return;
  }
  const sort = target.closest(".st");
  if (sort) {
    sorting = sort.dataset.sort;
    document.querySelectorAll(".st").forEach((item) =>
      item.setAttribute("aria-pressed", item === sort));
    apply();
    return;
  }
  const rowElement = target.closest(".row");
  if (rowElement) selectSoftware(rowElement.dataset.key, rowElement);
});

document.addEventListener("input", (event) => {
  if (event.target.id !== "q") return;
  clearTimeout(window.searchTimer);
  window.searchTimer = setTimeout(() => {
    queryText = query("#q").value.trim();
    apply();
  }, 500);
});

document.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  if (selectedKey) closeDetail();
});

async function boot() {
  await Promise.all([renderStats(), apply()]);
}

boot();
