// The colour section of the product form: one block per colour, each with
// its own photo list.
//
// Nothing reaches the server until the form is saved: photos chosen here
// are shown from the browser's memory, and reordering, the cover,
// removals and new colours only change what is on screen. Saving sends
// the fields, the colours and their photos together; the server applies
// all of it or none. If it refuses (a field to fix, a file that is not a
// photo), only the messages and the typed fields are swapped in from its
// answer, block by block, and the photos stay as arranged.
//
// Without JavaScript the form still posts, and each colour's new photos
// go after its existing ones (see rest/colour.go).
(() => {
  "use strict";

  const form = document.querySelector("[data-product-form]");
  const coloursBox = form && form.querySelector("[data-colours]");
  if (!coloursBox) return;

  const accepted = ["image/jpeg", "image/png", "image/webp"];
  const colourList = coloursBox.querySelector("[data-colour-list]");
  const template = coloursBox.querySelector("[data-colour-template]");
  const addRow = coloursBox.querySelector("[data-add-colour-row]");
  const addButton = coloursBox.querySelector("[data-add-colour]");
  const maxColours = Number(coloursBox.dataset.maxColours);
  const submit = form.querySelector("[data-submit]");
  const submitLabel = submit.textContent.trim();

  // Keys name a block's fields. They only grow: reusing the key of a
  // block taken off would mix two colours' fields in the form data.
  let nextKey = Number(coloursBox.dataset.nextKey);
  const sections = new Map(); // block key -> photo section
  let dirty = false;
  let saving = false;

  const el = (tag, attrs = {}, ...children) => {
    const node = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs)) {
      if (k === "onclick") node.addEventListener("click", v);
      else node.setAttribute(k, v);
    }
    node.append(...children);
    return node;
  };

  const button = (label, onclick, ariaLabel) =>
    el("button", { type: "button", class: "secondary", onclick, ...(ariaLabel ? { "aria-label": ariaLabel } : {}) }, label);

  const blocks = () => [...colourList.querySelectorAll("[data-colour]")];

  // photoSection runs the photo list of one colour block.
  function photoSection(block) {
    const key = block.dataset.colour;
    const section = block.querySelector("[data-photos]");
    const max = Number(section.dataset.max);
    const maxMB = Number(section.dataset.maxMb);
    const list = section.querySelector("[data-photo-list]");
    const empty = section.querySelector("[data-photo-empty]");
    const removedBox = section.querySelector("[data-photo-removed]");
    const status = section.querySelector("[data-photo-status]");
    const pickErrors = section.querySelector("[data-photo-pick-errors]");
    const input = section.querySelector("input[type=file]");

    // { kind: "existing", id, url } or { kind: "new", file, url }
    let items = [...list.querySelectorAll("[data-id]")].map((li) => ({
      kind: "existing",
      id: li.dataset.id,
      url: li.dataset.src,
    }));
    let removed = []; // existing items to delete on save
    let changedHere = false;

    // The files are sent from items, in their order, not from the field.
    input.removeAttribute("name");

    function changed() {
      changedHere = true;
      dirty = true;
      render();
    }

    function move(from, to) {
      const [item] = items.splice(from, 1);
      items.splice(to, 0, item);
      changed();
    }

    function remove(i) {
      const [item] = items.splice(i, 1);
      if (item.kind === "existing") removed.push(item);
      else URL.revokeObjectURL(item.url);
      changed();
    }

    function restore(i) {
      const [item] = removed.splice(i, 1);
      items.push(item);
      changed();
    }

    function render() {
      list.replaceChildren(
        ...items.map((item, i) => {
          const n = i + 1;
          const actions = el("div", { class: "photo-actions" });
          if (i === 0) actions.append(el("span", { class: "tag cover" }, "Capa"));
          if (item.kind === "new") actions.append(el("span", { class: "tag new" }, "Nova"));
          if (i > 0) {
            actions.append(button("Usar como capa", () => move(i, 0), `Usar a foto ${n} como capa`));
            actions.append(button("↑ Antes", () => move(i, i - 1), `Passar a foto ${n} para antes`));
          }
          if (i < items.length - 1) {
            actions.append(button("↓ Depois", () => move(i, i + 1), `Passar a foto ${n} para depois`));
          }
          actions.append(button("Tirar", () => remove(i), `Tirar a foto ${n}`));
          return el("li", { class: "photo" }, el("img", { src: item.url, alt: `Foto ${n}` }), actions);
        }),
      );
      empty.hidden = items.length > 0;

      removedBox.replaceChildren();
      if (removed.length > 0) {
        removedBox.append(
          el("div", { class: "removed-list" },
            el("h5", {}, "Serão apagadas ao salvar"),
            el("ol", { class: "photos" },
              ...removed.map((item, i) =>
                el("li", { class: "photo removed" },
                  el("img", { src: item.url, alt: "Foto que será apagada" }),
                  el("div", { class: "photo-actions" }, button("Desfazer", () => restore(i), "Manter esta foto")),
                ),
              ),
            ),
          ),
        );
      }

      const full = items.length >= max;
      input.disabled = full;
      status.hidden = !changedHere;
      status.textContent = changedHere
        ? `As mudanças nas fotos desta cor ainda não foram salvas. Elas só valem depois de “${submitLabel}”.` +
          (full ? ` Esta cor está com ${max} fotos, o máximo.` : "")
        : "";
    }

    input.addEventListener("change", () => {
      const problems = [];
      const left = [];
      for (const file of input.files) {
        if (!accepted.includes(file.type)) {
          problems.push(`${file.name}: não é uma foto em JPEG, PNG ou WebP.`);
        } else if (file.size > maxMB * 1024 * 1024) {
          problems.push(`${file.name}: passa de ${maxMB} MB.`);
        } else if (items.length >= max) {
          left.push(file.name);
        } else {
          items.push({ kind: "new", file, url: URL.createObjectURL(file) });
          changedHere = true;
          dirty = true;
        }
      }
      if (left.length > 0) {
        problems.push(`Cada cor pode ter até ${max} fotos; ficaram de fora: ${left.join(", ")}.`);
      }
      input.value = "";
      pickErrors.replaceChildren(
        ...(problems.length === 0 ? [] : [el("div", { class: "alert", role: "alert" }, ...problems.map((p) => el("p", {}, p)))]),
      );
      render();
    });

    render();
    return {
      // collect adds the colour's photos to data, as rest/colour.go reads
      // them, and returns how many files it added.
      collect(data) {
        const order = [];
        let newCount = 0;
        for (const item of items) {
          if (item.kind === "existing") {
            order.push("id:" + item.id);
          } else {
            order.push("new:" + newCount++);
            data.append("photos_" + key, item.file, item.file.name);
          }
        }
        data.set("photo_order_" + key, order.join(","));
        for (const item of removed) data.append("photo_remove_" + key, item.id);
        return newCount;
      },
      drop() {
        for (const item of items) if (item.kind === "new") URL.revokeObjectURL(item.url);
      },
    };
  }

  // Quantities are asked only for the sizes ticked above.
  function syncSizes() {
    const ticked = new Set([...form.querySelectorAll("input[name=sizes]:checked")].map((i) => i.value));
    for (const field of coloursBox.querySelectorAll("[data-stock-size]")) {
      field.hidden = !ticked.has(field.dataset.stockSize);
    }
    for (const note of coloursBox.querySelectorAll("[data-stock-empty]")) note.hidden = ticked.size > 0;
  }

  // The colour codes. Pickers stay hidden without JavaScript, since only
  // this script copies their choice into the code field, which is what is
  // sent. The page also offers spare empty rows for adding codes without
  // JavaScript; here they give way to the "+" button. Run again whenever
  // a block's fields come back from the server.
  function setUpHexes() {
    for (const box of coloursBox.querySelectorAll("[data-hexes]")) {
      for (const spare of box.querySelectorAll("[data-hex-spare]")) spare.remove();
      for (const picker of box.querySelectorAll("[data-hex-picker]")) picker.hidden = false;
      syncHexes(box);
    }
  }

  function syncHexes(box) {
    const rows = box.querySelectorAll("[data-hex-row]");
    rows.forEach((row, i) => {
      row.querySelector("[data-hex-text]").setAttribute("aria-label", i === 0 ? "Código da cor principal" : "Código de uma cor secundária");
      row.querySelector("[data-drop-hex]").hidden = i === 0;
    });
    const add = box.querySelector("[data-add-hex]");
    add.hidden = false;
    add.disabled = rows.length >= Number(box.dataset.max);
  }

  function addHex(box) {
    const row = box.querySelector("[data-hex-row]").cloneNode(true);
    const text = row.querySelector("[data-hex-text]");
    text.value = "";
    text.removeAttribute("aria-invalid");
    text.removeAttribute("aria-describedby");
    row.querySelector("[data-hex-picker]").value = "#ffffff";
    for (const error of row.querySelectorAll(".field-error")) error.remove();
    const rows = box.querySelectorAll("[data-hex-row]");
    rows[rows.length - 1].after(row);
    syncHexes(box);
    dirty = true;
    text.focus();
  }

  function dropHex(row) {
    const box = row.closest("[data-hexes]");
    row.remove();
    syncHexes(box);
    dirty = true;
    box.querySelector("[data-add-hex]").focus();
  }

  function syncBlocks() {
    const all = blocks();
    all.forEach((block, i) => {
      block.querySelector("[data-colour-title]").textContent = `Cor ${i + 1}`;
      const drop = block.querySelector("[data-drop-colour]");
      if (drop) drop.hidden = all.length <= 1;
      const removing = block.querySelector("[data-remove-colour]");
      block.classList.toggle("removing", Boolean(removing && removing.checked));
    });
    addButton.disabled = all.length >= maxColours;
  }

  function startBlock(block) {
    sections.set(block.dataset.colour, photoSection(block));
  }

  addButton.addEventListener("click", () => {
    const holder = document.createElement("div");
    holder.innerHTML = template.innerHTML.replaceAll("__N__", String(nextKey++));
    const block = holder.querySelector("[data-colour]");
    colourList.append(block);
    startBlock(block);
    syncBlocks();
    syncSizes();
    setUpHexes();
    dirty = true;
    block.querySelector("input[type=text]").focus();
  });

  colourList.addEventListener("click", (e) => {
    const addHexButton = e.target.closest("[data-add-hex]");
    if (addHexButton) return addHex(addHexButton.closest("[data-hexes]"));
    const dropHexButton = e.target.closest("[data-drop-hex]");
    if (dropHexButton) return dropHex(dropHexButton.closest("[data-hex-row]"));

    const drop = e.target.closest("[data-drop-colour]");
    if (!drop) return;
    const block = drop.closest("[data-colour]");
    sections.get(block.dataset.colour).drop();
    sections.delete(block.dataset.colour);
    block.remove();
    syncBlocks();
    dirty = true;
    addButton.focus();
  });

  form.addEventListener("change", (e) => {
    if (e.target.matches("input[name=sizes]")) syncSizes();
    if (e.target.matches("[data-remove-colour]")) syncBlocks();
  });
  form.addEventListener("input", (e) => {
    dirty = true;
    const box = e.target.closest("[data-hex-row]");
    if (!box) return;
    const picker = box.querySelector("[data-hex-picker]");
    const text = box.querySelector("[data-hex-text]");
    if (e.target === picker) {
      text.value = picker.value;
      return;
    }
    // The picker follows a code typed or pasted once it is a whole one.
    let code = text.value.trim().toLowerCase();
    if (code && !code.startsWith("#")) code = "#" + code;
    if (/^#[0-9a-f]{6}$/.test(code)) picker.value = code;
  });

  window.addEventListener("beforeunload", (e) => {
    if (!dirty || saving) return;
    e.preventDefault();
    e.returnValue = ""; // still needed by some browsers to ask
  });

  function showAlert(text) {
    form.querySelector("[data-form-messages]").replaceChildren(el("p", { class: "alert", role: "alert" }, text));
    form.scrollIntoView({ behavior: "smooth", block: "start" });
  }

  // swapIn replaces the region of the form matching selector with the
  // answer's, when the answer has one.
  function swapIn(doc, selector, within = form) {
    const fresh = doc.querySelector(selector);
    const current = within.querySelector(selector);
    if (fresh && current) current.replaceWith(fresh);
  }

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    if (saving) return;

    const data = new FormData(form);
    let newCount = 0;
    for (const section of sections.values()) newCount += section.collect(data);

    saving = true;
    submit.disabled = true;
    submit.textContent = newCount > 0 ? `Salvando e enviando ${newCount} ${newCount === 1 ? "foto" : "fotos"}…` : "Salvando…";

    try {
      // HX-Request asks the server to say where to go next in a header
      // instead of redirecting, as it does for HTMX.
      const res = await fetch(form.action, { method: "POST", body: data, headers: { "HX-Request": "true" } });
      const next = res.headers.get("HX-Redirect");
      if (next) {
        dirty = false;
        window.location.assign(next);
        return;
      }

      const doc = new DOMParser().parseFromString(await res.text(), "text/html");
      if (!doc.querySelector("[data-fields]")) {
        const title = doc.querySelector("h1");
        const text = doc.querySelector("main p");
        showAlert([title && title.textContent, text && text.textContent].filter(Boolean).join(". ") ||
          "Não consegui salvar agora. Nada foi salvo: tente de novo em instantes.");
        return;
      }
      for (const region of ["[data-form-messages]", "[data-fields]", "[data-colour-errors]"]) swapIn(doc, region);
      // The colour blocks themselves stay, photos and all: only what was
      // typed in them and their messages come from the answer.
      for (const block of blocks()) {
        const key = block.dataset.colour;
        swapIn(doc, `[data-colour-fields="${key}"]`, block);
        swapIn(doc, `[data-photo-errors="${key}"]`, block);
      }
      syncBlocks();
      syncSizes();
      setUpHexes();
      form.scrollIntoView({ behavior: "smooth", block: "start" });
    } catch {
      showAlert("Não consegui falar com o painel. Confira a internet e tente de novo. Nada foi salvo.");
    } finally {
      saving = false;
      submit.disabled = false;
      submit.textContent = submitLabel;
    }
  });

  for (const block of blocks()) startBlock(block);
  addRow.hidden = false;
  syncBlocks();
  syncSizes();
  setUpHexes();
})();
