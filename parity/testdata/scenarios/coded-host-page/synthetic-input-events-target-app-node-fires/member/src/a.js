export const type = (key) =>
  document.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true }));
window.document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }));
const root = document.body;
const ev = new InputEvent('input', { bubbles: true });
root.dispatchEvent(ev);
