// Framing uses the same CSS variables as saved library previews and board tokens.
// Keep native multipart uploads; only zoom and position are sent as extra fields.
(() => {
    const editors = new WeakMap();

    function initialize(form) {
        if (!form?.isConnected || editors.has(form)) return;
        const editor = form.querySelector('#image-fit-editor');
        if (!editor) return;
        const preview = form.querySelector('#token-image-preview');
        const artwork = preview?.querySelector('img');
        const fileInput = form.querySelector('#unit-image');
        const zoom = form.querySelector('#image-zoom');
        const zoomValue = form.querySelector('#image-zoom-value');
        const offsetX = form.querySelector('#image-offset-x');
        const offsetY = form.querySelector('#image-offset-y');
        const error = form.querySelector('#image-fit-error');
        const fitButton = form.querySelector('#fit-image');
        const centerButton = form.querySelector('#center-image');
        // A failed template render or a partial swap can omit editor controls.
        // Do not attach any listeners until the entire editor is available.
        if (![preview, artwork, fileInput, zoom, zoomValue, offsetX, offsetY, error, fitButton, centerButton].every(Boolean)) {
            if (error) {
                error.textContent = 'The unit form could not load completely. Restart the app and reopen this menu.';
                error.hidden = false;
            }
            return;
        }
        let preserveFit = editor.dataset.preserveFit === 'true';
        let objectURL = null;
        let version = 0;
        let disposed = false;
        let loading = false;
        let drag = null;

        function render() {
            preview.style.setProperty('--image-zoom', zoom.value);
            preview.style.setProperty('--image-offset-x', `${offsetX.value}%`);
            preview.style.setProperty('--image-offset-y', `${offsetY.value}%`);
            zoomValue.value = `${Math.round(Number(zoom.value) * 100)}%`;
        }

        function position(x, y) {
            offsetX.value = Math.max(-50, Math.min(50, x)).toFixed(2);
            offsetY.value = Math.max(-50, Math.min(50, y)).toFixed(2);
            render();
        }

        function reset() {
            zoom.value = '1';
            position(0, 0);
        }

        function showError(message) {
            error.textContent = message;
            error.hidden = !message;
            fileInput.setCustomValidity(message);
        }

        async function loadArtwork(resetFit) {
            const requestVersion = ++version;
            loading = true;
            showError('');
            fileInput.setCustomValidity('Please wait for the image preview to load.');
            const file = fileInput.files[0];
            const unitType = form.querySelector('input[name="unitType"]:checked')?.value || 'pc';
            if (file && file.size > 5 * 1024 * 1024) {
                loading = false;
                showError('The image must be 5 MB or smaller.');
                return;
            }
            if (file && !['image/png', 'image/jpeg', 'image/gif'].includes(file.type)) {
                loading = false;
                showError('Choose a PNG, JPEG, or GIF image.');
                return;
            }
            const nextURL = file ? URL.createObjectURL(file) : null;
            const image = new Image();
            image.src = nextURL || `/images/${unitType === 'pc' ? 'knight' : 'goblin'}`;
            try {
                await image.decode();
                if (disposed || requestVersion !== version) {
                    if (nextURL) URL.revokeObjectURL(nextURL);
                    return;
                }
                if (image.naturalWidth > 4096 || image.naturalHeight > 4096) {
                    throw new Error('Image dimensions must be 4096 × 4096 pixels or smaller.');
                }
                artwork.src = image.src;
                if (objectURL) URL.revokeObjectURL(objectURL);
                objectURL = nextURL;
                if (resetFit) reset();
                else render();
                showError('');
            } catch (failure) {
                if (nextURL) URL.revokeObjectURL(nextURL);
                if (disposed || requestVersion !== version) return;
                showError(failure.message.startsWith('Image dimensions') ? failure.message : 'This image could not be previewed. Please choose another image.');
            } finally {
                if (requestVersion === version) loading = false;
            }
        }

        fileInput.addEventListener('change', () => {
            loadArtwork(!preserveFit);
            preserveFit = false;
        });
        form.querySelectorAll('input[name="unitType"]').forEach(input => {
            input.addEventListener('change', () => {
                if (!fileInput.files.length) loadArtwork(false);
            });
        });
        zoom.addEventListener('input', render);
        fitButton.addEventListener('click', reset);
        centerButton.addEventListener('click', () => position(0, 0));

        preview.addEventListener('pointerdown', event => {
            if (event.button !== 0 || loading) return;
            event.preventDefault();
            preview.focus({preventScroll: true});
            preview.setPointerCapture(event.pointerId);
            drag = {id: event.pointerId, x: event.clientX, y: event.clientY, offsetX: Number(offsetX.value), offsetY: Number(offsetY.value)};
            preview.classList.add('is-dragging');
        });
        preview.addEventListener('pointermove', event => {
            if (!drag || drag.id !== event.pointerId) return;
            const rect = preview.getBoundingClientRect();
            position(drag.offsetX + (event.clientX - drag.x) / rect.width * 100,
                drag.offsetY + (event.clientY - drag.y) / rect.height * 100);
        });
        function stopDrag(event) {
            if (!drag || drag.id !== event.pointerId) return;
            drag = null;
            preview.classList.remove('is-dragging');
            if (preview.hasPointerCapture(event.pointerId)) preview.releasePointerCapture(event.pointerId);
        }
        preview.addEventListener('pointerup', stopDrag);
        preview.addEventListener('pointercancel', stopDrag);
        preview.addEventListener('lostpointercapture', stopDrag);
        preview.addEventListener('keydown', event => {
            const step = event.shiftKey ? 5 : 1;
            const directions = {ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step]};
            const delta = directions[event.key];
            if (!delta) return;
            event.preventDefault();
            position(Number(offsetX.value) + delta[0], Number(offsetY.value) + delta[1]);
        });

        editors.set(form, () => {
            disposed = true;
            version++;
            if (objectURL) URL.revokeObjectURL(objectURL);
        });
        render();
        loadArtwork(false);
    }

    function initializeWithin(root) {
        initialize(root.closest?.('#create-unit-form') || root.querySelector?.('#create-unit-form'));
    }
    document.addEventListener('htmx:load', event => initializeWithin(event.detail.elt));
    document.addEventListener('htmx:beforeCleanupElement', event => {
        const dispose = editors.get(event.detail.elt);
        if (dispose) dispose();
    });
    initializeWithin(document);
})();
