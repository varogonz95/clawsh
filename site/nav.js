// Tablets and phones: below 900px the header's nav moves into a side panel
// opened from a menu button (the CSS hides the nav once the page has the
// "js" class). Without JS the nav stays in the header and wraps.
(() => {
  const nav = document.querySelector(".top nav");
  if (!nav) return;
  const icon = (d) =>
    `<svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="${d}"/></svg>`;

  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = "menu-btn";
  btn.setAttribute("aria-label", "Open menu");
  btn.setAttribute("aria-haspopup", "dialog");
  btn.setAttribute("aria-controls", "menu");
  btn.setAttribute("aria-expanded", "false");
  btn.innerHTML = icon("M4 7h16M4 12h16M4 17h16");

  const menu = document.createElement("dialog");
  menu.id = "menu";
  menu.className = "menu";
  menu.setAttribute("aria-label", "Menu");
  const head = document.createElement("div");
  head.className = "menu-head";
  const close = document.createElement("button");
  close.type = "button";
  close.className = "menu-close";
  close.setAttribute("aria-label", "Close menu");
  close.innerHTML = icon("M6 6l12 12M18 6L6 18");
  head.append(document.querySelector(".top .mark").cloneNode(true), close);
  const links = nav.cloneNode(true);
  links.setAttribute("aria-label", "Menu");
  menu.append(head, links);

  nav.after(btn);
  document.body.append(menu);

  btn.addEventListener("click", () => {
    menu.showModal();
    btn.setAttribute("aria-expanded", "true");
  });
  menu.addEventListener("close", () => btn.setAttribute("aria-expanded", "false"));
  close.addEventListener("click", () => menu.close());
  // A click on the backdrop lands on the dialog itself; its content doesn't.
  menu.addEventListener("click", (e) => {
    if (e.target === menu || e.target.closest("a")) menu.close();
  });
  matchMedia("(min-width: 900px)").addEventListener("change", (e) => {
    if (e.matches && menu.open) menu.close();
  });
})();
