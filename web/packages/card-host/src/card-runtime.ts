import { CARD_BRIDGE_VERSION } from "./protocol";

export type InjectCardRuntimeOptions = {
  cardInstanceId: string;
  /** Per-channel nonce baked into the card; echoed back on card.ready. */
  nonce: string;
};

/**
 * Bridge script injected into the card HTML (srcdoc). It must stay plain
 * JavaScript without backticks, template placeholders or the closing script
 * tag, because it is embedded as an inline <script> inside a TS template
 * literal.
 */
const CARD_RUNTIME_SOURCE = [
  "(function (config) {",
  '  "use strict";',
  '  var VERSION = "argus.card-bridge.v1";',
  "  var sequence = 0;",
  "  var lastHostSequence = 0;",
  "  var requestCounter = 0;",
  "  var pending = {};",
  "  var initPayload = null;",
  "  var contextPayload = null;",
  "  var initCallbacks = [];",
  "  var contextCallbacks = [];",
  "",
  "  function post(type, payload) {",
  "    sequence += 1;",
  "    var message = {",
  "      version: VERSION,",
  "      cardInstanceId: config.cardInstanceId,",
  "      nonce: config.nonce,",
  "      sequence: sequence,",
  "      type: type",
  "    };",
  "    if (payload) {",
  "      for (var key in payload) { message[key] = payload[key]; }",
  "    }",
  "    if (window.parent && window.parent !== window) {",
  '      window.parent.postMessage(message, "*");',
  "    }",
  "  }",
  "",
  "  function fire(callbacks, value) {",
  "    for (var i = 0; i < callbacks.length; i += 1) {",
  "      try { callbacks[i](value); } catch (error) {",
  "        setTimeout(function () { throw error; }, 0);",
  "      }",
  "    }",
  "  }",
  "",
  "  function emit(name, detail) {",
  "    window.dispatchEvent(new CustomEvent(name, { detail: detail }));",
  "  }",
  "",
  "  function settle(requestId, failed, data, error) {",
  "    var entry = pending[requestId];",
  "    if (!entry) { return; }",
  "    delete pending[requestId];",
  "    if (failed) {",
  '      entry.reject(new Error((error && error.message) || "card bridge error"));',
  "    } else {",
  "      entry.resolve(data);",
  "    }",
  "  }",
  "",
  '  window.addEventListener("message", function (event) {',
  "    var message = event.data;",
  '    if (!message || typeof message !== "object") { return; }',
  "    if (message.version !== VERSION) { return; }",
  "    if (message.cardInstanceId !== config.cardInstanceId) { return; }",
  "    if (message.nonce !== config.nonce) { return; }",
  '    if (typeof message.sequence !== "number" || message.sequence <= lastHostSequence) { return; }',
  "    lastHostSequence = message.sequence;",
  "    switch (message.type) {",
  '      case "host.init":',
  "        initPayload = {",
  "          context: message.context,",
  "          bindings: message.bindings,",
  "          initialData: message.initialData",
  "        };",
  "        contextPayload = message.context;",
  "        fire(initCallbacks, initPayload);",
  '        emit("argus:init", initPayload);',
  "        break;",
  '      case "host.context":',
  "        contextPayload = message.context;",
  "        fire(contextCallbacks, contextPayload);",
  '        emit("argus:context", contextPayload);',
  "        break;",
  '      case "query.result":',
  '      case "action.result":',
  "        settle(message.requestId, false, message.data, null);",
  "        break;",
  '      case "query.error":',
  '      case "action.error":',
  "        settle(message.requestId, true, null, message.error);",
  "        break;",
  "      default:",
  "        break;",
  "    }",
  "  });",
  "",
  "  function invoke(kind, bindingKey, bindingId, params) {",
  "    return new Promise(function (resolve, reject) {",
  "      requestCounter += 1;",
  '      var requestId = config.cardInstanceId + ":" + kind + ":" + requestCounter;',
  "      pending[requestId] = { resolve: resolve, reject: reject };",
  "      var payload = { requestId: requestId, params: params };",
  "      payload[bindingKey] = bindingId;",
  '      post(kind + ".invoke", payload);',
  "    });",
  "  }",
  "",
  "  function currentHeight() {",
  "    var body = document.body;",
  "    var root = document.documentElement;",
  "    return Math.ceil(Math.max(",
  "      body ? body.scrollHeight : 0,",
  "      root ? root.scrollHeight : 0,",
  "      body ? body.offsetHeight : 0,",
  "      root ? root.offsetHeight : 0",
  "    ));",
  "  }",
  "",
  "  var lastHeight = 0;",
  "  function reportHeight(height) {",
  '    var next = typeof height === "number" ? Math.ceil(height) : currentHeight();',
  "    if (next > 0 && next !== lastHeight) {",
  "      lastHeight = next;",
  '      post("card.resize", { height: next });',
  "    }",
  "  }",
  "",
  "  window.argus = {",
  "    bridgeVersion: VERSION,",
  "    cardInstanceId: config.cardInstanceId,",
  "    onInit: function (callback) {",
  "      if (initPayload) { callback(initPayload); } else { initCallbacks.push(callback); }",
  "    },",
  "    onContext: function (callback) {",
  "      contextCallbacks.push(callback);",
  "      if (contextPayload) { callback(contextPayload); }",
  "    },",
  "    invokeQuery: function (queryBindingId, params) {",
  '      return invoke("query", "queryBindingId", queryBindingId, params);',
  "    },",
  "    invokeAction: function (actionBindingId, params) {",
  '      return invoke("action", "actionBindingId", actionBindingId, params);',
  "    },",
  "    notify: function (level, message) {",
  '      post("host.notify", { level: level, message: message });',
  "    },",
  "    openLink: function (url) {",
  '      post("link.open", { url: url });',
  "    },",
  "    resize: function (height) {",
  "      reportHeight(height);",
  "    }",
  "  };",
  "",
  '  if (typeof ResizeObserver === "function" && document.documentElement) {',
  "    new ResizeObserver(function () { reportHeight(); }).observe(document.documentElement);",
  "  }",
  '  window.addEventListener("load", function () { reportHeight(); });',
  "",
  '  post("card.ready", {});',
  "})(",
].join("\n");

/**
 * Injects the host bridge runtime into raw card HTML so it can be used as an
 * iframe srcdoc. The script exposes `window.argus`, validates incoming host
 * messages (version, instance id, nonce, sequence), reports `card.ready`
 * immediately and auto-reports content height via ResizeObserver.
 */
export function injectCardRuntime(
  html: string,
  options: InjectCardRuntimeOptions,
): string {
  const config = JSON.stringify({
    cardInstanceId: options.cardInstanceId,
    nonce: options.nonce,
  }).replace(/</g, "\\u003c");
  const script =
    '<script data-argus-card-runtime="true">\n' +
    CARD_RUNTIME_SOURCE +
    config +
    ");\n</script>";

  const headMatch = /<head[^>]*>/i.exec(html);
  if (headMatch) {
    const at = headMatch.index + headMatch[0].length;
    return html.slice(0, at) + "\n" + script + html.slice(at);
  }
  const htmlMatch = /<html[^>]*>/i.exec(html);
  if (htmlMatch) {
    const at = htmlMatch.index + htmlMatch[0].length;
    return html.slice(0, at) + "\n" + script + html.slice(at);
  }
  return script + "\n" + html;
}

/** Marker used by tests and debugging to verify runtime injection. */
export const CARD_RUNTIME_MARKER = 'data-argus-card-runtime="true"';

export { CARD_BRIDGE_VERSION };
