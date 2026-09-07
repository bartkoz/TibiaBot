// The loop clock lives in a worker because the panel tab is in the background
// whenever the user is actually playing: browsers throttle setTimeout in
// hidden tabs to about 1 Hz and stop requestAnimationFrame entirely, while
// worker timers are exempt. A clock on the main thread would tick only while
// nobody was looking at the game.
let timer = null;

onmessage = event => {
  clearInterval(timer);
  timer = null;
  if (event.data?.stop) return;
  const intervalMS = Math.max(10, Number(event.data?.intervalMS) || 100);
  timer = setInterval(() => postMessage('tick'), intervalMS);
};
