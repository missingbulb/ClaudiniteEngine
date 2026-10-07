el.dispatchEvent(new MouseEvent('click'));
el.dispatchEvent(new view.KeyboardEvent('keydown', { key: 'a' }));
const ev = new PointerEvent('pointerdown', { bubbles: false });
node.dispatchEvent(ev);
