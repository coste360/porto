/*! Porto booking widget loader (m8-plan §10). Usage:
 *  <div data-porto="{slug}" data-lang="it"></div><script src="https://{host}/embed.js" async></script>
 * It creates the booking iframe, resizes it on porto:resize and re-dispatches porto:step / porto:booked as a
 * CustomEvent('porto:booking') on the container. Only messages from the Porto origin of this script are used. */
/* global window, document */
(function () {
  'use strict';
  var w = window;
  var d = document;
  if (w.PortoBooking) return;

  function scriptSrc() {
    var s = d.currentScript;
    if (s && s.src) return s.src;
    var all = d.getElementsByTagName('script');
    for (var i = all.length - 1; i >= 0; i--) {
      if (/\/embed\.js(\?|#|$)/.test(all[i].src)) return all[i].src;
    }
    return '';
  }

  var link = d.createElement('a');
  link.href = scriptSrc();
  var porto = link.protocol + '//' + link.host;
  if (!/^https?:\/\/[^/]+$/.test(porto)) return;

  var frames = [];

  function mount(el) {
    if (el.getAttribute('data-porto-mounted')) return;
    var slug = el.getAttribute('data-porto') || '';
    if (!/^[a-z0-9-]{3,63}$/.test(slug)) return;
    el.setAttribute('data-porto-mounted', '1');
    var q = new URLSearchParams({ embed: '1', parent: w.location.origin });
    var lang = el.getAttribute('data-lang');
    if (lang && /^[a-z]{2}$/.test(lang)) q.set('lang', lang);
    var f = d.createElement('iframe');
    f.src = porto + '/b/' + slug + '?' + q.toString();
    f.title = el.getAttribute('data-title') || 'Prenotazione posto barca';
    f.setAttribute('allow', 'payment');
    f.setAttribute('frameborder', '0');
    f.setAttribute('scrolling', 'no');
    f.width = '100%';
    f.height = el.getAttribute('data-height') || '640';
    el.appendChild(f);
    frames.push({ el: el, frame: f });
  }

  function scan() {
    var list = d.querySelectorAll('[data-porto]');
    for (var i = 0; i < list.length; i++) mount(list[i]);
  }

  w.addEventListener('message', function (ev) {
    if (ev.origin !== porto) return;
    var m = ev.data;
    if (!m || typeof m !== 'object' || typeof m.type !== 'string') return;
    var hit = null;
    for (var i = 0; i < frames.length; i++) {
      if (frames[i].frame.contentWindow === ev.source) hit = frames[i];
    }
    if (!hit) return;
    if (m.type === 'porto:resize') {
      var h = Math.round(Number(m.height));
      if (h >= 200 && h <= 20000) hit.frame.height = String(h);
      return;
    }
    var detail = null;
    if (m.type === 'porto:step' && typeof m.step === 'string') {
      detail = { type: 'step', step: m.step.slice(0, 32) };
    } else if (m.type === 'porto:booked' && typeof m.reference === 'string') {
      detail = { type: 'booked', reference: m.reference.slice(0, 16) };
    }
    if (detail)
      hit.el.dispatchEvent(new CustomEvent('porto:booking', { bubbles: true, detail: detail }));
  });

  w.PortoBooking = { mount: scan };
  if (d.readyState === 'loading') d.addEventListener('DOMContentLoaded', scan);
  else scan();
})();
