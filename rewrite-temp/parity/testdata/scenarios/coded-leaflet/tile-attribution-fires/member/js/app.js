const map = L.map('map');
L.tileLayer('https://t/{z}.png', {
  maxZoom: 19,
}).addTo(map);
L.tileLayer('https://t/é/{z}.png');
L.tileLayer('a,b', {
  bounds: [[1, 2]], 'maxZoom': 3 });
