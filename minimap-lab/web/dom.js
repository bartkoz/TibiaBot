// Everything the panel needs from a document: two lookups, one bit of
// geometry, and the two builders its dynamic rows are made of.

// point maps a pointer event onto a grid of the given size, clamped to it.
// The element's own box is what the ratio comes from, so a canvas scaled by
// CSS still answers in source pixels.
export function point(event, element, width, height) {
  const r = element.getBoundingClientRect();
  return {
    x: Math.max(0, Math.min(width - 1, Math.floor((event.clientX - r.left) * width / r.width))),
    y: Math.max(0, Math.min(height - 1, Math.floor((event.clientY - r.top) * height / r.height))),
  };
}

export function createDom(document) {
  const $ = id => document.getElementById(id);
  const num = id => Number($(id).value);

  // field appends a labelled input to a row and hands back the input. Rows
  // built with it are rebuilt wholesale on every change, so nobody but the
  // input's own listener ever looks it up again.
  function field(row, id, label, value, type, attrs = {}) {
    const wrap = document.createElement('label');
    wrap.textContent = label;
    const input = document.createElement('input');
    input.id = id;
    input.type = type;
    if (type === 'checkbox') input.checked = value; else input.value = value;
    for (const [k, v] of Object.entries(attrs)) input.setAttribute(k, v);
    wrap.append(input);
    row.append(wrap);
    return input;
  }

  function button(row, id, text) {
    const b = document.createElement('button');
    b.id = id;
    b.className = 'secondary';
    b.textContent = text;
    row.append(b);
    return b;
  }

  // `point` is imported straight from this module where it is needed; it is
  // not on this object as well, so there is one path to it rather than two.
  return {document, $, num, field, button};
}
