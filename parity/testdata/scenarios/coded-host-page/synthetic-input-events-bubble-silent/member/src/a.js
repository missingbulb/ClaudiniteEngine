el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
el.dispatchEvent(new CustomEvent('x'));
el.dispatchEvent(new MouseEvent('click', { ...init }));
const ev = new MouseEvent('click');
el.dispatchEvent(other);
this.e2 = new MouseEvent('c');
el.dispatchEvent(e2);
el.dispatchEvent(new WheelEvent('wheel', { bubbles: !0 }));
