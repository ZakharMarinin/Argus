/*
 * Argus dashboard — клиентский слой.
 *
 * Здесь пять вещей и ничего больше:
 *   1. пауза htmx-поллинга и счётчик возраста данных;
 *   2. график, который живёт отдельно от поллинга партиалов;
 *   3. переключатель темы;
 *   4. мелкие локальные интеракции (модалка, копирование сниппета);
 *   5. живость ленты совпадений: новые строки, раскрытые строки, счётчик.
 *
 * Данные на страницу приносит htmx. Состояния приложения в JS нет: всё, что
 * видно на экране, пришло с сервера.
 */
(function () {
  'use strict';

  var REDUCED_MOTION = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  // Через сколько секунд без успешного обмена считать данные устаревшими.
  var STALE_AFTER_S = 30;

  // Как часто перезапрашивать точки графика. Реже партиалов: почасовой ряд
  // не меняется чаще раза в минуту.
  var CHART_REFRESH_MS = 60000;

  /* ---------------------------------------------------------------- htmx */

  // Пауза. Пользовательские запросы (клик по фильтру, переход) проходят всегда —
  // тормозим только опрос по таймеру, у которого нет triggeringEvent.
  var paused = false;

  document.addEventListener('htmx:beforeRequest', function (evt) {
    var cfg = evt.detail && evt.detail.requestConfig;
    var isPoll = !cfg || !cfg.triggeringEvent;
    if (paused && isPoll) evt.preventDefault();
  });

  // Подсветка изменений. Партиал свапается каждые 10 секунд независимо от
  // того, изменилось ли что-нибудь, поэтому анимировать сам факт свапа нельзя:
  // экран, который мигает без повода, приучает на себя не смотреть. Сравниваем
  // текст ответа с тем, что уже на экране, и подсвечиваем только разницу.
  //
  // Сравнивается именно textContent, а не разметка: Alpine дописывает в DOM
  // свои атрибуты, и по HTML карточка настроения «менялась» бы каждый тик.
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

  // Каждый успешный обмен обнуляет счётчик «обновлено N с назад».
  document.addEventListener('htmx:afterSwap', function (evt) {
    if (window.Alpine && Alpine.store('argus')) Alpine.store('argus').seen();

    restore();

    if (freshID) {
      var fresh = document.getElementById(freshID);
      freshID = null;
      if (fresh) {
        // Перезапуск анимации: без снятия класса и чтения layout она не
        // сыграет второй раз на том же элементе.
        fresh.classList.remove('is-fresh');
        void fresh.offsetWidth;
        fresh.classList.add('is-fresh');
      }
    }

    // Если свап принёс новый canvas, его надо завести: карточка с графиком
    // сама не поллится, и без этого график остался бы пустым.
    var target = evt.target;
    if (!target || !target.querySelectorAll) return;
    target.querySelectorAll('canvas[data-chart]').forEach(function (canvas) {
      if (!charts.has(canvas) && window.Chart) load(canvas);
    });
  });

  /* --------------------------------------------------------------- Alpine */

  // На старых страницах истории поллинга нет намеренно, и молчание сервера
  // там - не потеря связи.
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

      // Дашборд мониторинга не имеет права молча показывать устаревшие данные
      // как свежие. Самый частый интервал поллинга — 10 с; если обмена не было
      // втрое дольше, сервер недоступен, и это должно быть видно.
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
          // Снятие с паузы должно сразу подтянуть данные, а не ждать следующий тик.
          // Партиалы слушают `refreshNow from:body` — одного события хватает на все.
          if (window.htmx) htmx.trigger(document.body, 'refreshNow');
          scan();
        }
      }
    });

    // Кнопка темы. Саму тему применяет инлайновый скрипт из <head> — он же
    // единственный источник правды, чтобы страница не мигала при загрузке.
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

  /* --------------------------------------------------------------- График */

  // Chart.js держим отдельно от htmx: партиалы поллятся каждые 10 секунд, и
  // пересоздание canvas на каждый свап убивало бы анимацию и подсказки.
  // Поэтому карточка с графиком не поллится, а данные догружаются сюда.

  var charts = new WeakMap();

  function token(name) {
    return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  }

  // palette отдаёт цвета рядов по именам токенов, которые приходят с сервера.
  // Сервер называет цвет ролью (accent, up, warn, down), а не значением —
  // иначе график не перекрашивался бы вместе с темой.
  function palette() {
    return {
      accent: token('--accent'),
      up: token('--up'),
      warn: token('--warn'),
      down: token('--down'),
      idle: token('--idle'),
      // Оформление самого графика читается с тёмной панели карточки, а не со
      // страницы: график всегда живёт на .card--dark, и --grid-line/--ink дали
      // бы белую сетку и почти чёрные подписи на чёрном фоне. Токена --muted в
      // палитре нет вовсе - раньше подписи осей оставались на умолчании
      // Chart.js (серый #666) и в светлой теме не читались.
      muted: token('--emphasis-fg-soft'),
      grid: token('--emphasis-border'),
      border: token('--emphasis-border'),
      surface: token('--emphasis-bg-hover'),
      ink: token('--emphasis-fg'),
      ink2: token('--emphasis-fg-soft')
    };
  }

  // hexToRgba нужен для заливки под линией: сам токен непрозрачный.
  function hexToRgba(hex, alpha) {
    var m = /^#([0-9a-f]{6})$/i.exec(hex);
    if (!m) return hex;
    var n = parseInt(m[1], 16);
    return 'rgba(' + [(n >> 16) & 255, (n >> 8) & 255, n & 255].join(',') + ',' + alpha + ')';
  }

  function datasets(payload, colors) {
    return payload.series.map(function (s) {
      var color = colors[s.color] || colors.accent;

      // События за час - счётчик, а не измерение: сглаженная линия рисовала бы
      // плавный подъём там, где событий не было вовсе. Такой ряд идёт
      // столбиками - видно ровно те часы, в которые что-то произошло.
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
        // Час без проверок приходит как null: разрыв честнее нарисованного нуля.
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
    // Ряд без единиц измерения - счётчик: деление «0.5 события» врёт.
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

    // За период нет ни одной точки — пустые оси врут об охвате данных,
    // поэтому вместо графика показываем прямую фразу.
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
        // График — не критичная часть страницы: пишем в консоль и не ломаем
        // всё остальное, если сервер не отдал данные.
        console.warn('[argus] график не загрузился:', err.message);
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

  // Смена темы — системная или ручная — перекрашивает оси, сетку и подсказки.
  // Проще пересобрать график, чем обходить все цвета в его конфиге.
  document.addEventListener('argus:theme', function () {
    document.querySelectorAll('canvas[data-chart]').forEach(function (canvas) {
      if (!charts.has(canvas)) return;
      destroy(canvas);
      load(canvas);
    });
  });

  /* ------------------------------------------- Раскрытые <details> --- */

  // <details> состояние не помнит: ссылки в сайдбаре обычные, при переходе
  // страница рендерится заново. Группа контейнеров и легенда настроений
  // помнят выбор там же, где тема, - ключ в data-remember.
  //
  // Вызывается сразу, а не по DOMContentLoaded: app.js подключён с defer, то
  // есть выполняется после разбора разметки - элементы уже на месте, и
  // состояние восстанавливается до отрисовки.
  document.querySelectorAll('details[data-remember]').forEach(function (el) {
    var key = el.dataset.remember;

    try {
      var saved = localStorage.getItem(key);
      if (saved) el.open = saved === 'open';
    } catch (e) { /* приватный режим - остаётся состояние из разметки */ }

    el.addEventListener('toggle', function () {
      try {
        localStorage.setItem(key, el.open ? 'open' : 'closed');
      } catch (e) { /* см. выше */ }
    });
  });

  /* ------------------------------------------------ Выпадающие меню --- */

  // Меню на <details> закрывается само только повторным кликом по кнопке.
  // Клик мимо и Escape - привычное поведение выпадающего списка.
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
})();
