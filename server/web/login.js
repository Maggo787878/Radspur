'use strict';
  const e = new URLSearchParams(location.search).get('err');
  if (e === '1') document.getElementById('err').hidden = false;
  if (e === 'cookie') document.getElementById('err-cookie').hidden = false;
  if (e === 'locked') document.getElementById('err-locked').hidden = false;
