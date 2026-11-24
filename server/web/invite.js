'use strict';
(async () => {
  const token = location.pathname.split('/').pop();
  const form = document.getElementById('form');
  const err = document.getElementById('err');
  let info;
  try {
    const res = await fetch('/api/invite/' + encodeURIComponent(token));
    if (!res.ok) throw new Error();
    info = await res.json();
  } catch {
    document.getElementById('invalid').hidden = false;
    return;
  }
  const reset = info.purpose === 'reset';
  document.getElementById('intro').textContent = reset
    ? `Neues Passwort für „${info.username}“ festlegen.`
    : 'Du wurdest zu Radspur eingeladen. Wähle Benutzername und Passwort.';
  document.getElementById('submit').textContent = reset ? 'Passwort speichern' : 'Konto anlegen';
  const user = form.username;
  if (reset) { document.getElementById('user-label').hidden = true; user.required = false; }
  else user.required = true;
  form.hidden = false;
  (reset ? form.password : user).focus();

  form.onsubmit = async e => {
    e.preventDefault();
    err.hidden = true;
    if (form.password.value !== form.password2.value) {
      err.textContent = 'Die Passwörter stimmen nicht überein.'; err.hidden = false; return;
    }
    const res = await fetch('/api/invite/' + encodeURIComponent(token), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Radspur': '1' },
      body: JSON.stringify({ username: reset ? '' : user.value.trim(), password: form.password.value }),
    });
    if (res.ok) { location.replace('/'); return; }
    err.textContent = (await res.text()).trim() || 'Fehler ' + res.status;
    err.hidden = false;
  };
})();
