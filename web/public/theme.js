// Applies the saved appearance before the first paint, so a dark choice never flashes light.
// Kept as a same-origin file because the CSP does not allow inline scripts.
;(function () {
  var preference = ''
  try {
    preference = localStorage.getItem('modemdeck.theme') || ''
  } catch (error) {}
  var dark =
    preference === 'dark' ||
    (preference !== 'light' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.setAttribute('data-theme', dark ? 'dark' : 'light')
})()
