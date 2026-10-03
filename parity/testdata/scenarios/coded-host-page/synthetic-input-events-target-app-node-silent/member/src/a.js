cell.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true }));
document.dispatchEvent(new CustomEvent('x'));
const root = document.querySelector('#app');
root.dispatchEvent(new MouseEvent('click', { bubbles: true }));
