const Ctor = key ? view.KeyboardEvent : view.MouseEvent;
// é unicode é ✓
const e = new Ctor(type, { ...init, bubbles: !1 });
node.dispatchEvent( e );
