import { ssr } from './__ssr_gen__';

const errorEl = document.getElementById('display-name-error');

ssr.onError((varName: string, message: string) => {
    if (varName === 'displayName' && errorEl) {
        errorEl.textContent = message;
        setTimeout(() => { if (errorEl) errorEl.textContent = ''; }, 4000);
    }
});

ssr.on('displayName', () => {
    if (errorEl) errorEl.textContent = '';
});
