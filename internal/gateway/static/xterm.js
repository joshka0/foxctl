/* xterm.js v5.x placeholder — in production, replace with actual xterm.js from npm */
/**
 * Minimal xterm.js stub for build/embed verification.
 * For the full xterm.js experience, replace this file with the actual
 * xterm.js from https://cdn.jsdelivr.net/npm/xterm@5/dist/xterm.js
 */
(function(global) {
  'use strict';
  var Terminal = function(opts) {
    this.cols = 80;
    this.rows = 24;
    this._el = null;
    this._handlers = { data: [], resize: [] };
  };
  Terminal.prototype.open = function(el) { this._el = el; };
  Terminal.prototype.write = function(data) { /* stub */ };
  Terminal.prototype.onData = function(cb) { this._handlers.data.push(cb); };
  Terminal.prototype.onResize = function(cb) { this._handlers.resize.push(cb); };
  Terminal.prototype.loadAddon = function(addon) { /* stub */ };
  Terminal.prototype.dispose = function() { /* stub */ };
  global.Terminal = Terminal;

  var FitAddon = {};
  FitAddon.FitAddon = function() {
    this.fit = function() { /* stub */ };
    this.dispose = function() { /* stub */ };
  };
  global.FitAddon = FitAddon;
})(window);
