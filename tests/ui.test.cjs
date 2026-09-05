const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname, '../ui/app.js'), 'utf8');

async function app(label) {
  const events = new Map(), calls = [], elements = new Map();
  let actions = [];
  const timers = new Set();
  let timerID = 0;
  const element = () => ({ textContent: '', innerHTML: '', dataset: {}, style: {}, appendChild(){}, classList: {add(){},remove(){},toggle(){}},
    querySelectorAll: () => [], querySelector: () => null, addEventListener(name, fn) { this[name] = fn; } });
  for (const id of ['#orb', '#pop', '#tmr-display', '#tmr-status', '#ob-timer']) elements.set(id, element());
  const context = vm.createContext({ console, setInterval(){ const id = ++timerID; timers.add(id); return id; }, clearInterval(id){ timers.delete(id); }, setTimeout(){},
    document: { body: element(), createElement: element, querySelector: s => elements.get(s) || null,
      querySelectorAll: () => actions, addEventListener(){} },
    window: { __TAURI__: {core: { invoke: async (cmd, args) => {
      calls.push([cmd,args]);
      return cmd === 'get_config' ? {lang:'en',hidden_widgets:['monitor']} :
        cmd === 'pom_snapshot' ? {phase:'focus',running:true,paused:false,remaining_secs:123,focus_min:25,break_min:5,rounds_done:1} :
        cmd === 'shelf_list' ? [] : {};
    }}, event: {listen: async (name, fn) => {
      if (!events.has(name)) events.set(name, []);
      events.get(name).push(fn);
      return () => {};
    }}, window: {getCurrentWindow: () => ({label})}} }
  });
  const ready = vm.runInContext(source, context);
  await ready;
  return { context, events, calls, elements, timers,
    run: code => vm.runInContext(code,context),
    async click(act) {
      const el = element(); el.dataset.act = act; actions = [el];
      vm.runInContext('bindPopActions()', context);
      actions = [];
      await el.click();
    }
  };
}

test('orb respects persisted visibility on first render', async () => {
  const a = await app('orb');
  assert(!a.elements.get('#orb').innerHTML.includes('data-w="monitor"'));
});
test('orb rebuilds do not multiply backend event subscriptions', async () => {
  const a = await app('orb');
  a.run('buildOrb(); buildOrb()');
  assert.equal(a.events.get('visibility-changed').length, 1);
  assert.equal(a.events.get('tauri://drag-drop').length, 1);
});
test('shelf rerenders do not multiply file drop handlers', async () => {
  const a = await app('popover-shelf');
  a.run('renderPop(); renderPop()');
  assert.equal(a.events.get('tauri://drag-drop').length, 1);
});
test('timer loads current pomodoro and updates its visible time', async () => {
  const a = await app('popover-timer');
  assert(a.elements.get('#pop').innerHTML.includes('02:03'));
  await a.events.get('pomodoro')[0]({payload:{running:true,paused:false,phase:'focus',remaining_secs:120,focus_min:25,break_min:5,rounds_done:1}});
  assert(a.elements.get('#tmr-display').textContent === '02:00' || a.elements.get('#pop').innerHTML.includes('02:00'));
});
test('resuming stopwatch does not count paused elapsed twice', async () => {
  const a = await app('popover-timer');
  a.run('localTimer.mode="stopwatch"; localTimer.running=true; localTimer.paused=true; localTimer.pausedElapsed=10000; localTimer.startAt=null');
  await a.click('sw_resume');
  const elapsed = a.run('timerElapsedMs()');
  assert(elapsed >= 10000 && elapsed < 11000, `elapsed=${elapsed}`);
});
test('AI app launch button invokes backend', async () => {
  const a = await app('popover-ai_apps');
  await a.click('ai_app');
  assert(a.calls.some(([cmd]) => cmd === 'open_gui_app'));
});

test('orb reveals rings bottom-up, top-down, bottom-up', async () => {
  const a = await app('orb');
  for (const count of [11, 10, 9, 5, 1]) {
    a.run(`S.config.hidden_widgets = ORB_ORDER.slice(${count - 1}, -1); buildOrb()`);
    const html = a.elements.get('#orb').innerHTML;
    const items = [...html.matchAll(/data-i="(\d+)"\s+style="[^"]*?(?:transition-delay|--reveal-delay):(\d+)ms/g)]
      .map(m => ({ index: Number(m[1]), delay: Number(m[2]) }));
    const expected = [];
    for (let start = 0; start < count; start += 4) {
      const end = Math.min(start + 4, count);
      for (let i = end - 1; i >= start; i--) expected.push(i);
    }
    assert.equal(items.length, count);
    assert.deepEqual(items.sort((a,b) => a.delay-b.delay).map(x => x.index), expected);
  }
});


test('countdown ticks update text without rebuilding the popover', async () => {
  const a = await app('popover-timer');
  const html = a.elements.get('#pop').innerHTML;
  await a.events.get('pomodoro')[0]({payload:{running:true,paused:false,phase:'focus',remaining_secs:120,focus_min:25,break_min:5,rounds_done:1}});
  assert.equal(a.elements.get('#pop').innerHTML, html);
  assert.equal(a.elements.get('#tmr-display').textContent, '02:00');
});


test('local timer schedules ticks only while running', async () => {
  const a = await app('popover-timer');
  assert.equal(a.timers.size, 0);
  await a.click('mode_timer');
  await a.click('tmr_start');
  assert.equal(a.timers.size, 1);
  await a.click('tmr_pause');
  assert.equal(a.timers.size, 0);
  await a.click('tmr_resume');
  assert.equal(a.timers.size, 1);
  await a.click('tmr_reset');
  assert.equal(a.timers.size, 0);
});

test('orb collapse reverses reveals and outer ring fills from one end', async () => {
  const a = await app('orb');
  a.run('S.config.hidden_widgets = []; buildOrb()');
  for (const count of [9, 10, 11]) {
    const positions = a.run(`orbLayout(Array(${count}).fill('widget'))`);
    const angles = positions.slice(8).map(p => Math.round(Math.atan2(264-p.y, p.x-336)*180/Math.PI));
    assert.deepEqual(Array.from(angles).reverse(), [172, 147, 122].slice(0, count-8));
  }
  const html = a.elements.get('#orb').innerHTML;
  const delays = [...html.matchAll(/--reveal-delay:(\d+)ms; --hide-delay:(\d+)ms/g)];
  assert.equal(delays.length, 11);
  assert.ok(delays.every(m => Number(m[1]) + Number(m[2]) === 450));
});
