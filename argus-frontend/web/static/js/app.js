/*
 * Argus dashboard: the client-side layer.
 *
 * There are six things here and nothing more:
 *   1. pausing htmx polling and the data age counter;
 *   2. the chart, which lives apart from partial polling;
 *   3. the theme switcher;
 *   4. small local interactions (the modal, copying a snippet);
 *   5. liveliness of the match feed: new rows, expanded rows, the counter;
 *   6. the overview board: the person's own order and widths of the blocks.
 *
 * Data is brought to the page by htmx. There is no application state in JS:
 * every number on screen came from the server. The only things kept here are
 * presentation choices (theme, open groups, the overview layout), and they
 * live in localStorage.
 */
(function () {
  'use strict';

  var REDUCED_MOTION = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  // After how many seconds without a successful exchange the data counts as stale.
  var STALE_AFTER_S = 30;

  // How often to re-request the chart points. Less often than partials: the hourly
  // series doesn't change more than once a minute.
  var CHART_REFRESH_MS = 60000;

  /* ---------------------------------------------------------------- htmx */

  // Pause. User requests (a filter click, navigation) always go through:
  // we only hold back timer polling, which has no triggeringEvent.
  var paused = false;

  // A block is being dragged: a swap would change its height under the
  // pointer, so the timer polls wait for the drop.
  var holdPolls = false;

  document.addEventListener('htmx:beforeRequest', function (evt) {
    var cfg = evt.detail && evt.detail.requestConfig;
    var isPoll = !cfg || !cfg.triggeringEvent;
    if ((paused || holdPolls) && isPoll) evt.preventDefault();
  });

  // Change highlighting. A partial is swapped every 10 seconds whether or not
  // anything changed, so the swap itself must not be animated: a screen that
  // blinks for no reason teaches people not to look at it. We compare the
  // response text with what is already on screen and highlight only the difference.
  //
  // It is textContent that is compared, not the markup: Alpine adds its own
  // attributes to the DOM, and by HTML the mood card would "change" on every tick.
  var freshID = null;
  var memory = null;

  function squash(text) {
    return text.replace(/\s+/g, ' ').trim();
  }

  function rowIDs(root, selector) {
    var out = new Set();
    root.querySelectorAll(selector).forEach(function (el) { out.add(el.dataset.rowId); });
    return out;
  }

  function remember(target) {
    var counter = target.querySelector('[data-count]');
    memory = {
      id: target.id,
      count: counter ? Number(counter.dataset.count) : null,
      rows: rowIDs(target, '[data-row-id]'),
      open: rowIDs(target, 'details[open][data-row-id]')
    };
  }

  function countUp(el, from, to) {
    if (from === to || REDUCED_MOTION) return;
    var start = null;
    function frame(t) {
      if (start === null) start = t;
      var k = Math.min((t - start) / 700, 1);
      el.textContent = Math.round(from + (to - from) * (1 - Math.pow(1 - k, 3)));
      if (k < 1) requestAnimationFrame(frame);
    }
    el.textContent = from;
    requestAnimationFrame(frame);
  }

  function restore() {
    var mem = memory;
    memory = null;
    var root = mem && document.getElementById(mem.id);
    if (!root) return;

    root.querySelectorAll('[data-row-id]').forEach(function (row) {
      var id = row.dataset.rowId;
      if (mem.open.has(id)) row.open = true;
      else if (!mem.rows.has(id)) row.classList.add('is-new');
    });

    var counter = root.querySelector('[data-count]');
    if (counter && mem.count !== null) countUp(counter, mem.count, Number(counter.dataset.count));
  }

  document.addEventListener('htmx:beforeSwap', function (evt) {
    var d = evt.detail;
    if (!d || !d.shouldSwap || !d.target || !d.target.id) return;
    if (typeof d.serverResponse !== 'string') return;

    remember(d.target);

    var next = document.createElement('template');
    next.innerHTML = d.serverResponse;
    if (squash(next.content.textContent) !== squash(d.target.textContent)) {
      freshID = d.target.id;
    }
  });

  // Every successful exchange resets the "updated N s ago" counter.
  document.addEventListener('htmx:afterSwap', function (evt) {
    if (window.Alpine && Alpine.store('argus')) Alpine.store('argus').seen();

    restore();

    if (freshID) {
      var fresh = document.getElementById(freshID);
      freshID = null;
      if (fresh) {
        // Restart the animation: without removing the class and reading layout it
        // won't play a second time on the same element.
        fresh.classList.remove('is-fresh');
        void fresh.offsetWidth;
        fresh.classList.add('is-fresh');
      }
    }

    // If the swap brought a new canvas, it has to be started: the chart card
    // doesn't poll by itself, and without this the chart would stay empty.
    var target = evt.target;
    if (!target || !target.querySelectorAll) return;
    target.querySelectorAll('canvas[data-chart]').forEach(function (canvas) {
      if (!charts.has(canvas) && window.Chart) load(canvas);
    });
  });

  /* --------------------------------------------------------------- Alpine */

  // Older history pages deliberately don't poll, and server silence
  // there isn't a lost connection.
  function polling() {
    return document.querySelector('[hx-trigger*="every"]') !== null;
  }

  document.addEventListener('alpine:init', function () {
    Alpine.store('argus', {
      paused: false,
      seconds: 0,
      modal: false,

      init: function () {
        var self = this;
        setInterval(function () {
          if (!self.paused) self.seconds += 1;
        }, 1000);
      },

      get label() {
        if (this.paused) return 'auto-refresh paused';
        if (!polling()) return 'not auto-refreshing';
        if (this.stale) return 'lost connection to server · ' + this.seconds + 's';
        if (this.seconds < 2) return 'updated just now';
        return 'updated ' + this.seconds + 's ago';
      },

      // A monitoring dashboard has no right to silently show stale data
      // as fresh. The most frequent polling interval is 10 s; if there was no
      // exchange for three times that, the server is unavailable, and that must be visible.
      get stale() {
        return !this.paused && this.seconds > STALE_AFTER_S && polling();
      },

      seen: function () {
        this.seconds = 0;
      },

      toggle: function () {
        this.paused = !this.paused;
        paused = this.paused;
        if (!this.paused) {
          this.seconds = 0;
          // Unpausing must pull in data right away, not wait for the next tick.
          // Partials listen to `refreshNow from:body`: one event is enough for all of them.
          if (window.htmx) htmx.trigger(document.body, 'refreshNow');
          scan();
        }
      }
    });

    // Theme button. The theme itself is applied by the inline script in <head>, which
    // is also the single source of truth, so the page doesn't flash on load.
    Alpine.data('themeToggle', function () {
      return {
        mode: (window.argusTheme && window.argusTheme.mode) || 'auto',

        get title() {
          return {
            auto: 'Theme: same as system. Click to switch to light',
            light: 'Theme: light. Click to switch to dark',
            dark: 'Theme: dark. Click to go back to system'
          }[this.mode];
        },

        cycle: function () {
          var next = { auto: 'light', light: 'dark', dark: 'auto' }[this.mode];
          window.argusTheme.set(next);
          this.mode = next;
        }
      };
    });
  });

  /* --------------------------------------------------------------- Chart */

  // Chart.js is kept apart from htmx: partials poll every 10 seconds, and
  // recreating the canvas on every swap would kill the animation and tooltips.
  // So the chart card doesn't poll, and the data is loaded here.

  var charts = new WeakMap();

  function token(name) {
    return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  }

  // palette returns series colours by the token names that come from the server.
  // The server names a colour by role (accent, up, warn, down), not by value,
  // otherwise the chart wouldn't change colour together with the theme.
  function palette() {
    return {
      accent: token('--accent'),
      up: token('--up'),
      warn: token('--warn'),
      down: token('--down'),
      idle: token('--idle'),
      // The chart's own styling is read from the card's dark panel, not from
      // the page: the chart always lives on .card--dark, and --grid-line/--ink would
      // give a white grid and almost black labels on a black background. There is no
      // --muted token in the palette at all: axis labels used to stay at the Chart.js
      // default (grey #666) and were unreadable in the light theme.
      muted: token('--emphasis-fg-soft'),
      grid: token('--emphasis-border'),
      border: token('--emphasis-border'),
      surface: token('--emphasis-bg-hover'),
      ink: token('--emphasis-fg'),
      ink2: token('--emphasis-fg-soft')
    };
  }

  // hexToRgba is needed for the fill under the line: the token itself is opaque.
  function hexToRgba(hex, alpha) {
    var m = /^#([0-9a-f]{6})$/i.exec(hex);
    if (!m) return hex;
    var n = parseInt(m[1], 16);
    return 'rgba(' + [(n >> 16) & 255, (n >> 8) & 255, n & 255].join(',') + ',' + alpha + ')';
  }

  function datasets(payload, colors) {
    return payload.series.map(function (s) {
      var color = colors[s.color] || colors.accent;

      // Events per hour are a counter, not a measurement: a smoothed line would draw
      // a gentle rise where there were no events at all. Such a series is drawn
      // as bars, showing exactly the hours in which something happened.
      if (s.style === 'bar') {
        return {
          type: 'bar',
          label: s.label,
          data: s.values,
          backgroundColor: color,
          borderWidth: 0,
          borderRadius: 3,
          maxBarThickness: 14,
          yAxisID: s.axis === 'right' ? 'y1' : 'y'
        };
      }

      var area = s.style === 'area';
      return {
        label: s.label,
        data: s.values,
        borderColor: color,
        backgroundColor: area ? hexToRgba(color, 0.14) : 'transparent',
        fill: area,
        borderWidth: area ? 2.5 : 2,
        tension: 0.35,
        pointRadius: 0,
        yAxisID: s.axis === 'right' ? 'y1' : 'y',
        // An hour without checks arrives as null: a gap is more honest than a drawn zero.
        spanGaps: false
      };
    });
  }

  function axis(colors, font, unit, right) {
    var scale = {
      grid: right ? { display: false } : { color: colors.grid, drawTicks: false },
      border: { display: false },
      ticks: {
        color: colors.muted, font: font, maxTicksLimit: 5,
        callback: function (v) { return unit ? v + unit : v; }
      },
      beginAtZero: true
    };
    if (right) {
      scale.position = 'right';
      scale.beginAtZero = false;
    }
    if (unit === '%') {
      scale.suggestedMax = 100;
      scale.beginAtZero = false;
    }
    // A series without units is a counter: a "0.5 events" tick is a lie.
    if (!unit) {
      scale.ticks.precision = 0;
    }
    return scale;
  }

  function options(payload, colors) {
    var font = { family: 'Inter, system-ui, sans-serif', size: 11 };
    var left = payload.series.find(function (s) { return s.axis !== 'right'; });
    var right = payload.series.find(function (s) { return s.axis === 'right'; });

    var scales = {
      x: {
        grid: { display: false },
        border: { color: colors.border },
        ticks: { color: colors.muted, font: font, maxTicksLimit: 7 }
      },
      y: axis(colors, font, left ? left.unit : '', false)
    };
    if (right) scales.y1 = axis(colors, font, right.unit, true);
    if (payload.stacked) {
      scales.x.stacked = true;
      scales.y.stacked = true;
    }

    return {
      responsive: true,
      maintainAspectRatio: false,
      animation: REDUCED_MOTION ? false : { duration: 600 },
      interaction: { mode: 'index', intersect: false },
      plugins: {
        legend: { display: false },
        tooltip: {
          backgroundColor: colors.surface, titleColor: colors.ink, bodyColor: colors.ink2,
          borderColor: colors.border, borderWidth: 1,
          padding: 10, cornerRadius: 10,
          titleFont: font, bodyFont: font, footerFont: font, footerColor: colors.ink,
          displayColors: true, boxWidth: 8, boxHeight: 8,
          callbacks: payload.stacked ? { footer: total } : {}
        }
      },
      scales: scales
    };
  }

  function total(items) {
    var sum = items.reduce(function (n, item) { return n + (item.parsed.y || 0); }, 0);
    return 'total: ' + sum;
  }

  function emptyNoteFor(canvas) {
    var box = canvas.closest('.chart-box');
    return box && box.querySelector('[data-chart-empty]');
  }

  function draw(canvas, payload) {
    var note = emptyNoteFor(canvas);

    // Not a single point for the period: empty axes lie about data coverage,
    // so we show a plain sentence instead of the chart.
    if (payload.empty) {
      destroy(canvas);
      canvas.hidden = true;
      if (note) note.hidden = false;
      return;
    }
    canvas.hidden = false;
    if (note) note.hidden = true;

    var colors = palette();
    var existing = charts.get(canvas);

    if (existing) {
      existing.data.labels = payload.labels;
      payload.series.forEach(function (s, i) {
        if (existing.data.datasets[i]) existing.data.datasets[i].data = s.values;
      });
      existing.update();
      return;
    }

    charts.set(canvas, new Chart(canvas, {
      type: 'line',
      data: { labels: payload.labels, datasets: datasets(payload, colors) },
      options: options(payload, colors)
    }));
  }

  function destroy(canvas) {
    var chart = charts.get(canvas);
    if (chart) {
      chart.destroy();
      charts.delete(canvas);
    }
  }

  function load(canvas) {
    fetch(canvas.dataset.chart, { headers: { Accept: 'application/json' } })
      .then(function (r) {
        if (!r.ok) throw new Error('HTTP ' + r.status);
        return r.json();
      })
      .then(function (payload) { draw(canvas, payload); })
      .catch(function (err) {
        // The chart isn't a critical part of the page: log to the console and don't
        // break everything else if the server didn't return data.
        console.warn('[argus] chart failed to load:', err.message);
      });
  }

  function scan() {
    document.querySelectorAll('canvas[data-chart]').forEach(function (canvas) {
      if (window.Chart) load(canvas);
    });
  }

  document.addEventListener('DOMContentLoaded', function () {
    scan();
    setInterval(function () {
      if (!paused) scan();
    }, CHART_REFRESH_MS);
  });

  // A theme change, system or manual, recolours the axes, grid and tooltips.
  // It's simpler to rebuild the chart than to walk all the colours in its config.
  document.addEventListener('argus:theme', function () {
    document.querySelectorAll('canvas[data-chart]').forEach(function (canvas) {
      if (!charts.has(canvas)) return;
      destroy(canvas);
      load(canvas);
    });
  });

  /* ------------------------------------------- Expanded <details> --- */

  // <details> doesn't remember its state: the sidebar links are plain, and on
  // navigation the page is rendered anew. The container group and the mood legend
  // remember the choice in the same place as the theme, keyed by data-remember.
  //
  // Called immediately, not on DOMContentLoaded: app.js is loaded with defer,
  // so it runs after the markup is parsed: the elements are already in place, and
  // the state is restored before paint.
  document.querySelectorAll('details[data-remember]').forEach(function (el) {
    var key = el.dataset.remember;

    try {
      var saved = localStorage.getItem(key);
      if (saved) el.open = saved === 'open';
    } catch (e) { /* private mode: the state from the markup stays */ }

    el.addEventListener('toggle', function () {
      try {
        localStorage.setItem(key, el.open ? 'open' : 'closed');
      } catch (e) { /* see above */ }
    });
  });

  /* ------------------------------------------------ Dropdown menus --- */

  // A <details> menu closes by itself only on a second click on the button.
  // Clicking outside and Escape are the usual dropdown behaviour.
  function closeDropdowns(except) {
    document.querySelectorAll('details[data-dropdown][open]').forEach(function (d) {
      if (d !== except) d.open = false;
    });
  }

  document.addEventListener('click', function (evt) {
    closeDropdowns(evt.target.closest && evt.target.closest('details[data-dropdown]'));
  });

  document.addEventListener('keydown', function (evt) {
    if (evt.key !== 'Escape') return;
    var open = document.querySelector('details[data-dropdown][open]');
    if (!open) return;
    open.open = false;
    open.querySelector('summary').focus();
  });
  /* -------------------------------------------------- Overview board --- */

  // The person arranges the overview: order, width and hidden blocks. The
  // server always renders the default arrangement; the saved one lives in
  // localStorage next to the theme and is applied right here, before paint
  // (app.js is deferred, see the <details> note above).
  //
  // Only presentation is stored, never data: block ids, widths in grid
  // columns, and which blocks are hidden or folded.

  var LAYOUT_KEY = 'argus:layout';
  var LAYOUT_VERSION = 1;

  // How far the pointer has to travel before a press on the handle becomes a
  // drag. Less than this is a click or a shaky hand.
  var DRAG_THRESHOLD_PX = 4;

  // Auto-scroll while dragging near the top or bottom of the window.
  var SCROLL_EDGE_PX = 72;
  var SCROLL_MAX_PX = 18;

  var board = document.getElementById('board');
  if (board) initBoard(board);

  function initBoard(board) {
    var editbar = board.querySelector('[data-layout-bar]');
    var tray = board.querySelector('[data-layout-tray]');
    var note = board.querySelector('[data-layout-note]');
    var noteText = board.querySelector('[data-layout-note-text]');
    var live = board.querySelector('[data-layout-live]');
    var resetButton = board.querySelector('[data-layout-reset]');
    var customize = document.querySelector('.page-head [data-layout-edit]');

    var editing = false;
    var drag = null;  // pointer drag in progress
    var grab = null;  // keyboard "pick up" in progress
    var moving = false;  // our own DOM move, which blurs the focused handle

    function all() {
      return Array.prototype.slice.call(board.querySelectorAll(':scope > [data-block]'));
    }

    function shown() {
      return all().filter(function (b) { return !b.hasAttribute('data-hidden'); });
    }

    function find(id) {
      return board.querySelector(':scope > [data-block="' + id + '"]');
    }

    function sizesOf(block) {
      return Array.prototype.map.call(block.querySelectorAll('[data-layout-size]'), function (b) {
        return b.dataset.layoutSize;
      });
    }

    function titleOf(block) {
      return block.dataset.title;
    }

    var defaults = all().map(function (b) {
      return { id: b.dataset.block, size: b.dataset.defaultSize };
    });

    /* --- reading and writing the arrangement --- */

    function current() {
      var size = {};
      var hidden = [];
      var folded = [];
      all().forEach(function (b) {
        size[b.dataset.block] = b.dataset.size;
        if (b.hasAttribute('data-hidden')) hidden.push(b.dataset.block);
        if (b.hasAttribute('data-folded')) folded.push(b.dataset.block);
      });

      return {
        v: LAYOUT_VERSION,
        order: all().map(function (b) { return b.dataset.block; }),
        size: size,
        hidden: hidden,
        folded: folded
      };
    }

    function isDefault(layout) {
      if (layout.hidden.length || layout.folded.length) return false;

      return defaults.every(function (d, i) {
        return layout.order[i] === d.id && layout.size[d.id] === d.size;
      });
    }

    function load() {
      try {
        var saved = JSON.parse(localStorage.getItem(LAYOUT_KEY));
        if (saved && saved.v === LAYOUT_VERSION && Array.isArray(saved.order)) return saved;
      } catch (e) { /* private mode or a broken value: the default stays */ }

      return null;
    }

    function save() {
      var layout = current();
      try {
        // A layout equal to the default isn't stored: whoever never changed
        // anything keeps getting the default, even when it changes.
        if (isDefault(layout)) localStorage.removeItem(LAYOUT_KEY);
        else localStorage.setItem(LAYOUT_KEY, JSON.stringify(layout));
      } catch (e) { /* the change simply won't survive a reload */ }
    }

    // apply puts the blocks into the given arrangement. The saved layout is
    // only a wish: blocks the server no longer renders are dropped, and a
    // block it added later lands at its default place instead of vanishing.
    function apply(layout) {
      var ids = defaults.map(function (d) { return d.id; });
      var order = (layout ? layout.order : ids).filter(function (id, i, list) {
        return ids.indexOf(id) >= 0 && list.indexOf(id) === i;
      });
      ids.forEach(function (id, i) {
        if (order.indexOf(id) < 0) order.splice(Math.min(i, order.length), 0, id);
      });

      order.forEach(function (id) { board.appendChild(find(id)); });

      defaults.forEach(function (d) {
        var block = find(d.id);
        var size = layout && layout.size ? String(layout.size[d.id]) : d.size;
        block.dataset.size = sizesOf(block).indexOf(size) >= 0 ? size : d.size;

        var hide = !!layout && Array.isArray(layout.hidden) && layout.hidden.indexOf(d.id) >= 0;
        block.toggleAttribute('data-hidden', hide && !block.hasAttribute('data-pinned'));
        block.toggleAttribute('data-folded', !!layout && Array.isArray(layout.folded) && layout.folded.indexOf(d.id) >= 0);
      });

      sync();
    }

    /* --- the controls follow the arrangement --- */

    function sync() {
      var hidden = all().filter(function (b) { return b.hasAttribute('data-hidden'); });

      all().forEach(function (block) {
        block.querySelectorAll('[data-layout-size]').forEach(function (button) {
          button.setAttribute('aria-pressed', String(button.dataset.layoutSize === block.dataset.size));
        });
      });

      // The fold toggle lives in a partial that swaps itself back to the
      // server's markup; its state comes from the block wrapper.
      board.querySelectorAll('[data-fold-toggle]').forEach(function (button) {
        button.setAttribute('aria-expanded', String(!button.closest('[data-block]').hasAttribute('data-folded')));
      });

      board.querySelectorAll('[data-layout-show]').forEach(function (button) {
        button.hidden = !find(button.dataset.layoutShow).hasAttribute('data-hidden');
      });
      tray.hidden = hidden.length === 0;

      note.hidden = editing || hidden.length === 0;
      noteText.textContent = 'Hidden on this overview: ' + hidden.map(titleOf).join(', ') + '.';

      resetButton.disabled = isDefault(current());
    }

    /* --- packing --- */

    // Masonry: each child of the board spans as many 1px rows as it is tall
    // (see the board CSS), so a block neither stretches to its neighbour nor
    // leaves a hole under itself. It is redone whenever a block changes height:
    // a poll brought more rows, a width changed, a folder opened.
    var gap = parseFloat(getComputedStyle(board).rowGap) || 0;
    var packer = new ResizeObserver(function (entries) {
      entries.forEach(function (e) {
        e.target.style.gridRowEnd = 'span ' + Math.max(1, Math.ceil(e.borderBoxSize[0].blockSize + gap));
      });
    });
    Array.prototype.forEach.call(board.children, function (el) { packer.observe(el); });
    board.setAttribute('data-packed', '');

    function say(text) {
      live.textContent = text;
    }

    function position(block) {
      var list = shown();

      return 'position ' + (list.indexOf(block) + 1) + ' of ' + list.length;
    }

    /* --- motion --- */

    // flip animates the blocks from where they were to where a DOM change
    // put them, so a reorder reads as movement instead of a jump.
    function flip(change) {
      if (REDUCED_MOTION) {
        change();

        return;
      }

      var els = Array.prototype.slice.call(board.querySelectorAll(':scope > [data-block], :scope > .block-placeholder'))
        .filter(function (el) { return !el.classList.contains('is-dragging'); });
      var before = new Map();
      els.forEach(function (el) {
        before.set(el, el.getBoundingClientRect());
        el.getAnimations().forEach(function (a) { a.cancel(); });
      });

      change();

      els.forEach(function (el) {
        var a = before.get(el);
        var b = el.getBoundingClientRect();
        var dx = a.left - b.left;
        var dy = a.top - b.top;
        if (!b.width || (Math.abs(dx) < 1 && Math.abs(dy) < 1)) return;
        el.animate(
          [{ transform: 'translate(' + dx + 'px, ' + dy + 'px)' }, { transform: 'none' }],
          { duration: 240, easing: 'cubic-bezier(.16, 1, .3, 1)' }
        );
      });
    }

    /* --- edit mode --- */

    function setEditing(on) {
      if (editing === on) return;
      if (!on) {
        if (grab) drop();
        if (drag) finishDrag(true);
      }

      editing = on;
      board.toggleAttribute('data-editing', on);
      editbar.hidden = !on;
      if (customize) customize.setAttribute('aria-pressed', String(on));

      // While arranging, the content is a preview: links and buttons inside
      // must not catch clicks or focus.
      all().forEach(function (block) {
        block.querySelector('.block__body').inert = on;
      });

      sync();

      if (on) {
        var first = shown()[0];
        if (first) first.querySelector('[data-layout-handle]').focus();
      } else if (customize) {
        customize.focus();
      }
    }

    if (customize) customize.hidden = false;
    document.querySelectorAll('[data-layout-edit]').forEach(function (button) {
      button.addEventListener('click', function () { setEditing(!editing); });
    });

    board.querySelector('[data-layout-done]').addEventListener('click', function () {
      setEditing(false);
    });

    resetButton.addEventListener('click', function () {
      flip(function () {
        try { localStorage.removeItem(LAYOUT_KEY); } catch (e) { /* nothing to remove */ }
        apply(null);
      });
      board.querySelector('[data-layout-done]').focus();
      say('Overview layout reset to default.');
    });

    /* --- width and visibility --- */

    board.addEventListener('click', function (evt) {
      if (!editing || !evt.target.closest) return;
      var block = evt.target.closest('[data-block]');

      var size = evt.target.closest('[data-layout-size]');
      if (size && block) {
        flip(function () { block.dataset.size = size.dataset.layoutSize; });
        save();
        sync();
        say(titleOf(block) + ': ' + size.getAttribute('aria-label').toLowerCase() + '.');

        return;
      }

      var hide = evt.target.closest('[data-layout-hide]');
      if (hide && block) {
        var list = shown();
        var next = list[list.indexOf(block) + 1] || list[list.indexOf(block) - 1];
        flip(function () { block.setAttribute('data-hidden', ''); });
        save();
        sync();
        if (next) next.querySelector('[data-layout-handle]').focus();
        say(titleOf(block) + ' hidden. It can be brought back from the bar at the top.');

        return;
      }

      var show = evt.target.closest('[data-layout-show]');
      if (show) {
        var target = find(show.dataset.layoutShow);
        flip(function () { target.removeAttribute('data-hidden'); });
        save();
        sync();
        target.querySelector('[data-layout-handle]').focus();
        say(titleOf(target) + ' is shown again, ' + position(target) + '.');
      }
    });

    /* --- keyboard: pick up, move, put down --- */

    function pick(block) {
      grab = { block: block, order: current().order };
      block.classList.add('is-grabbed');
      say(titleOf(block) + ' picked up, ' + position(block) +
        '. Arrow keys move it, Space drops it, Escape cancels.');
    }

    function drop() {
      var block = grab.block;
      grab = null;
      block.classList.remove('is-grabbed');
      save();
      sync();
      say(titleOf(block) + ' dropped at ' + position(block) + '.');
    }

    function cancelGrab() {
      var block = grab.block;
      var order = grab.order;
      grab = null;
      block.classList.remove('is-grabbed');
      refocus(block, function () {
        flip(function () { order.forEach(function (id) { board.appendChild(find(id)); }); });
      });
      say('Move cancelled. ' + titleOf(block) + ' is back at ' + position(block) + '.');
    }

    // Moving a focused element in the DOM takes focus away from it; the
    // handle gets it back, and the blur in between isn't a "drop".
    function refocus(block, change) {
      moving = true;
      change();
      block.querySelector('[data-layout-handle]').focus();
      moving = false;
    }

    function shift(block, delta) {
      var list = shown();
      var to = list.indexOf(block) + delta;
      if (to < 0 || to >= list.length) {
        say(titleOf(block) + ' is already ' + (to < 0 ? 'first' : 'last') + '.');

        return;
      }

      refocus(block, function () {
        flip(function () {
          board.insertBefore(block, delta < 0 ? list[to] : list[to].nextSibling);
        });
      });
      say(titleOf(block) + ', ' + position(block) + '.');
    }

    board.addEventListener('keydown', function (evt) {
      var handle = editing && evt.target.closest && evt.target.closest('[data-layout-handle]');
      if (!handle || drag) return;
      var block = handle.closest('[data-block]');

      if (evt.key === ' ' || evt.key === 'Enter') {
        evt.preventDefault();
        if (grab && grab.block === block) drop();
        else {
          if (grab) drop();
          pick(block);
        }

        return;
      }

      if (!grab || grab.block !== block) return;

      var step = { ArrowUp: -1, ArrowLeft: -1, ArrowDown: 1, ArrowRight: 1 }[evt.key];
      if (step) {
        evt.preventDefault();
        shift(block, step);
      } else if (evt.key === 'Home' || evt.key === 'End') {
        evt.preventDefault();
        var list = shown();
        shift(block, evt.key === 'Home' ? -list.indexOf(block) : list.length - 1 - list.indexOf(block));
      } else if (evt.key === 'Escape') {
        evt.preventDefault();
        cancelGrab();
      }
    });

    // Tabbing away with a block in hand puts it down where it is.
    board.addEventListener('focusout', function (evt) {
      if (grab && !moving && evt.target === grab.block.querySelector('[data-layout-handle]')) drop();
    });

    /* --- pointer: drag by the handle --- */

    board.addEventListener('pointerdown', function (evt) {
      var handle = editing && !drag && evt.button === 0 && evt.target.closest &&
        evt.target.closest('[data-layout-handle]');
      if (!handle) return;
      if (grab) drop();

      drag = {
        block: handle.closest('[data-block]'),
        handle: handle,
        pointer: evt.pointerId,
        x0: evt.clientX,
        y0: evt.clientY,
        x: evt.clientX,
        y: evt.clientY,
        started: false
      };
      handle.setPointerCapture(evt.pointerId);
    });

    board.addEventListener('pointermove', function (evt) {
      if (!drag || drag.settling || evt.pointerId !== drag.pointer) return;
      drag.x = evt.clientX;
      drag.y = evt.clientY;

      if (!drag.started) {
        if (Math.hypot(drag.x - drag.x0, drag.y - drag.y0) < DRAG_THRESHOLD_PX) return;
        startDrag();
      }

      follow();
      place();
    });

    board.addEventListener('pointerup', function (evt) {
      if (!drag || drag.settling || evt.pointerId !== drag.pointer) return;
      if (drag.started) finishDrag(true);
      else drag = null;
    });

    board.addEventListener('pointercancel', function (evt) {
      if (!drag || drag.settling || evt.pointerId !== drag.pointer) return;
      if (drag.started) finishDrag(false);
      else drag = null;
    });

    function startDrag() {
      var block = drag.block;
      var rect = block.getBoundingClientRect();

      drag.started = true;
      drag.dx = drag.x0 - rect.left;
      drag.dy = drag.y0 - rect.top;
      drag.from = current().order;

      // The placeholder takes the block's cell, with the same width and
      // height, so nothing around it jumps when the block lifts off.
      var holder = document.createElement('div');
      holder.className = 'block-placeholder';
      holder.dataset.size = block.dataset.size;
      holder.style.height = rect.height + 'px';
      holder.setAttribute('aria-hidden', 'true');
      board.insertBefore(holder, block);
      packer.observe(holder);
      drag.holder = holder;

      // Only the width is fixed: in hand the block shrinks to a preview.
      block.style.width = rect.width + 'px';
      block.style.left = rect.left + 'px';
      block.style.top = rect.top + 'px';
      block.classList.add('is-dragging');
      document.documentElement.classList.add('is-dragging-block');

      // `position: fixed` is relative to the window unless an ancestor
      // says otherwise; measure instead of assuming.
      var placed = block.getBoundingClientRect();
      drag.left = rect.left + (rect.left - placed.left);
      drag.top = rect.top + (rect.top - placed.top);
      block.style.left = drag.left + 'px';
      block.style.top = drag.top + 'px';
      drag.base = { left: rect.left, top: rect.top };

      holdPolls = true;
      drag.raf = requestAnimationFrame(scrollTick);
    }

    function follow() {
      var x = drag.x - drag.dx - drag.base.left;
      var y = drag.y - drag.dy - drag.base.top;
      drag.block.style.transform = 'translate(' + x + 'px, ' + y + 'px)';
    }

    function nextShown(el) {
      var n = el.nextElementSibling;
      while (n && (n === drag.block || n.hasAttribute('data-hidden'))) n = n.nextElementSibling;

      return n;
    }

    // place moves the placeholder under the pointer. A wide block is split
    // into top and bottom halves, a narrow one into left and right: that is
    // the direction its neighbours sit in. Over the placeholder itself
    // nothing happens, which is what keeps it from flickering back and forth.
    function place() {
      var boardRect = board.getBoundingClientRect();
      var others = shown().filter(function (b) { return b !== drag.block; });
      var ref;

      for (var i = 0; i < others.length; i++) {
        var r = others[i].getBoundingClientRect();
        if (drag.x < r.left || drag.x > r.right || drag.y < r.top || drag.y > r.bottom) continue;
        var wide = r.width > boardRect.width * 0.66;
        var before = wide ? drag.y < r.top + r.height / 2 : drag.x < r.left + r.width / 2;
        ref = before ? others[i] : nextShown(others[i]);
        break;
      }

      // Below every block: to the end. In a packed board the last block
      // isn't necessarily the lowest one.
      if (ref === undefined) {
        var bottom = Math.max.apply(null, others.map(function (b) { return b.getBoundingClientRect().bottom; }));
        if (!others.length || drag.y <= bottom) return;
        ref = null;
      }

      if (nextShown(drag.holder) === ref) return;
      flip(function () { board.insertBefore(drag.holder, ref); });
    }

    function scrollTick() {
      if (!drag || !drag.started || drag.settling) return;
      var v = 0;
      if (drag.y < SCROLL_EDGE_PX) v = -(SCROLL_EDGE_PX - drag.y) / SCROLL_EDGE_PX * SCROLL_MAX_PX;
      else if (drag.y > window.innerHeight - SCROLL_EDGE_PX) {
        v = (drag.y - window.innerHeight + SCROLL_EDGE_PX) / SCROLL_EDGE_PX * SCROLL_MAX_PX;
      }
      if (v) {
        window.scrollBy(0, v);
        place();
      }
      drag.raf = requestAnimationFrame(scrollTick);
    }

    // finishDrag lands the block in the placeholder's cell, or, when the
    // drag was cancelled, back in its own.
    function finishDrag(commit) {
      var d = drag;
      d.settling = true;
      cancelAnimationFrame(d.raf);
      if (d.handle.hasPointerCapture(d.pointer)) d.handle.releasePointerCapture(d.pointer);

      if (!commit) {
        flip(function () { board.insertBefore(d.holder, d.block); });
      }

      var target = d.holder.getBoundingClientRect();
      var x = target.left - d.base.left;
      var y = target.top - d.base.top;

      function land() {
        board.insertBefore(d.block, d.holder);
        packer.unobserve(d.holder);
        d.holder.remove();
        d.block.classList.remove('is-dragging', 'is-settling');
        ['width', 'left', 'top', 'transform'].forEach(function (p) { d.block.style[p] = ''; });
        document.documentElement.classList.remove('is-dragging-block');
        holdPolls = false;
        drag = null;

        var moved = current().order.join() !== d.from.join();
        if (commit && moved) {
          save();
          say(titleOf(d.block) + ' moved to ' + position(d.block) + '.');
        }
        sync();
      }

      if (REDUCED_MOTION) {
        land();

        return;
      }

      d.block.classList.add('is-settling');
      d.block.style.transform = 'translate(' + x + 'px, ' + y + 'px)';
      var done = false;
      function once() {
        if (done) return;
        done = true;
        land();
      }
      d.block.addEventListener('transitionend', once, { once: true });
      setTimeout(once, 300);
    }

    // Escape: drop a dragged block back where it came from; otherwise leave
    // edit mode. A keyboard grab handles its own Escape above.
    document.addEventListener('keydown', function (evt) {
      if (evt.key !== 'Escape' || evt.defaultPrevented || !editing) return;
      if (drag && drag.started && !drag.settling) {
        finishDrag(false);

        return;
      }
      if (!drag) setEditing(false);
    });

    /* --- folding (outside edit mode too: it's a way of reading) --- */

    board.addEventListener('click', function (evt) {
      var button = !editing && evt.target.closest && evt.target.closest('[data-fold-toggle], [data-fold-open]');
      if (!button) return;
      var block = button.closest('[data-block]');
      var fold = button.hasAttribute('data-fold-toggle') && !block.hasAttribute('data-folded');

      block.toggleAttribute('data-folded', fold);
      save();
      sync();

      if (fold) {
        say(titleOf(block) + ' folded. Containers that need attention stay in view.');

        return;
      }

      // The folder button is gone: focus goes to the first card it let out.
      if (!REDUCED_MOTION) {
        block.classList.add('is-unfolding');
        setTimeout(function () { block.classList.remove('is-unfolding'); }, 400);
      }
      if (button.hasAttribute('data-fold-open')) {
        var first = block.querySelector('.ctn-card--quiet');
        if (first) first.focus();
      }
      say(titleOf(block) + ' shows all containers.');
    });

    // A partial on the board swapped itself (outerHTML: the event comes from
    // the new markup, the old target is already detached).
    board.addEventListener('htmx:afterSettle', sync);

    // Another tab changed the layout: follow it, unless this one is busy.
    window.addEventListener('storage', function (evt) {
      if (evt.key !== LAYOUT_KEY || drag || grab) return;
      flip(function () { apply(load()); });
    });

    apply(load());
  }
})();
