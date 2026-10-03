new chrome.declarativeContent.SetIcon({ imageData: await decode({ path: 'a.png' }) });
chrome.action.setIcon({ path: 'icons/on-16.png' });
// new chrome.declarativeContent.SetIcon({ path: 'x.png' })
new chrome.declarativeContent.SetIcon({ imageData: 'path: x' });
