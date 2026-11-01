import { CallError } from 'gossr-runtime';
import { ssr } from './__ssr_gen__';

const button = document.getElementById('refresh-last-seen');
const result = document.getElementById('refresh-last-seen-result');

button?.addEventListener('click', () => {
    ssr.call('refreshLastSeen', {})
        .then((out) => {
            if (result) result.textContent = 'Refreshed: ' + out.lastSeen;
        })
        .catch((err: unknown) => {
            if (result) result.textContent = err instanceof CallError ? err.message : 'The call failed.';
        });
});
