// The photo section of the product form.
//
// Nothing reaches the server until the form is saved: photos chosen here
// are shown from the browser's memory, and reordering, the cover and
// removals only change what is on screen. Saving sends the fields and the
// photos together; the server applies all of it or none. If it refuses
// (a field to fix, a file that is not a photo), only the messages are
// swapped in from its answer, and the photos stay as arranged.
//
// Without JavaScript the form still posts, and new photos go after the
// existing ones (see rest/photo.go).
(() => {
  "use strict";

  const form = document.querySelector("[data-product-form]");
  const section = form && form.querySelector("[data-photos]");
  if (!section) return;

  const max = Number(section.dataset.max);
  const maxMB = Number(section.dataset.maxMb);
  const accepted = ["image/jpeg", "image/png", "image/webp"];

  const list = section.querySelector("[data-photo-list]");
  const empty = section.querySelector("[data-photo-empty]");
  const removedBox = section.querySelector("[data-photo-removed]");
  const status = section.querySelector("[data-photo-status]");
  const pickErrors = section.querySelector("[data-photo-pick-errors]");
  const input = section.querySelector("input[type=file]");
  const submit = form.querySelector("[data-submit]");
  const submitLabel = submit.textContent;

  // { kind: "existing", id, url } or { kind: "new", file, url }
  let items = [...list.querySelectorAll("[data-id]")].map((li) => ({
    kind: "existing",
    id: li.dataset.id,
    url: li.dataset.src,
  }));
  let removed = []; // existing items to delete on save
  let dirty = false;
  let saving = false;

  // The files are sent from items, in their order, not from the field.
  input.removeAttribute("name");

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

  function changed() {
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
          el("h3", {}, "Serão apagadas ao salvar"),
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
    status.hidden = !dirty;
    status.textContent = dirty
      ? `As mudanças nas fotos ainda não foram salvas. Elas só valem depois de “${submitLabel.trim()}”.` +
        (full ? ` A peça está com ${max} fotos, o máximo.` : "")
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
        dirty = true;
      }
    }
    if (left.length > 0) {
      problems.push(`Cada peça pode ter até ${max} fotos; ficaram de fora: ${left.join(", ")}.`);
    }
    input.value = "";
    pickErrors.replaceChildren(
      ...(problems.length === 0 ? [] : [el("div", { class: "alert", role: "alert" }, ...problems.map((p) => el("p", {}, p)))]),
    );
    render();
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

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    if (saving) return;

    const data = new FormData(form);
    const order = [];
    let newCount = 0;
    for (const item of items) {
      if (item.kind === "existing") {
        order.push("id:" + item.id);
      } else {
        order.push("new:" + newCount++);
        data.append("photos", item.file, item.file.name);
      }
    }
    data.set("photo_order", order.join(","));
    for (const item of removed) data.append("photo_remove", item.id);

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
      const fields = doc.querySelector("[data-fields]");
      if (!fields) {
        const title = doc.querySelector("h1");
        const text = doc.querySelector("main p");
        showAlert([title && title.textContent, text && text.textContent].filter(Boolean).join(". ") ||
          "Não consegui salvar agora. Nada foi salvo: tente de novo em instantes.");
        return;
      }
      for (const region of ["[data-form-messages]", "[data-fields]", "[data-photo-errors]"]) {
        const fresh = doc.querySelector(region);
        if (fresh) form.querySelector(region).replaceWith(fresh);
      }
      form.scrollIntoView({ behavior: "smooth", block: "start" });
    } catch {
      showAlert("Não consegui falar com o painel. Confira a internet e tente de novo. Nada foi salvo.");
    } finally {
      saving = false;
      submit.disabled = false;
      submit.textContent = submitLabel;
    }
  });

  render();
})();
