/* Native application snapshot; browser pages cannot query NSWorkspace. */
(() => {
  const apps = window.VOICEMORE_APPS?.apps || [];
  const byID = new Map(apps.map(app => [app.bundleID, app]));
  const aliases = { Codex: 'com.openai.codex', appCodex: 'com.openai.codex', Lark: 'com.electron.lark', appFeishu: 'com.electron.lark', appBrowser: 'com.apple.Safari', appNotes: 'com.apple.Notes' };
  const esc = text => String(text ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  window.appIdentity = (name, bundleID) => {
    const matches = apps.filter(app => app.name === name);
    const app = bundleID ? byID.get(bundleID) : byID.get(aliases[name]) || (matches.length === 1 ? matches[0] : null);
    const label = app?.name || name || 'App';
    return `<span class="app-identity" title="${esc(label)}" data-bundle-id="${esc(app?.bundleID || '')}"><span class="app-icon-wrap"><span class="app-icon-fallback" aria-hidden="true">▧</span>${app ? `<img class="native-app-icon" src="assets/apps/${encodeURIComponent(app.icon)}" alt="" onerror="this.remove()">` : ''}</span><span>${esc(label)}</span></span>`;
  };
})();
