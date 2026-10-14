(() => {
    let dispose = () => {};

    function setup() {
        dispose();
        dispose = () => {};
        const form = document.getElementById('create-map-form');
        if (!form) return;
        const file = document.getElementById('map-image');
        const image = document.getElementById('map-preview-image');
        const width = document.getElementById('map-width');
        const height = document.getElementById('map-height');
        const x = document.getElementById('map-offset-x');
        const y = document.getElementById('map-offset-y');
        const zoom = document.getElementById('map-zoom');
        const zoomValue = document.getElementById('map-zoom-value');
        const fit = document.getElementById('fit-map-image');
        const center = document.getElementById('center-map-image');
        const preview = document.getElementById('map-preview');
        const error = document.getElementById('map-preview-error');
        let url = null;
        let baseWidth = 20;
        let baseHeight = 20;
        let drag = null;
        const clamp = value => Math.max(0.1, Math.min(64, value));
        const round = value => Number(value.toFixed(3));

        function render() {
            for (const [property, input] of Object.entries({width, height, left: x, top: y})) {
                image.style[property] = `${Number(input.value) * 16}px`;
            }
            zoomValue.value = `${Math.round(Number(zoom.value) * 100)}%`;
        }
        function resize() {
            width.value = round(baseWidth * Number(zoom.value));
            height.value = round(baseHeight * Number(zoom.value));
            render();
        }
        zoom.addEventListener('input', resize);
        fit.addEventListener('click', () => {
            zoom.value = '1';
            x.value = y.value = '0';
            resize();
            preview.scrollTo(0, 0);
        });
        center.addEventListener('click', () => {
            x.value = round((64 - Number(width.value)) / 2);
            y.value = round((64 - Number(height.value)) / 2);
            render();
            preview.scrollTo({left: 32 * 16 - preview.clientWidth / 2,
                top: 32 * 16 - preview.clientHeight / 2});
        });
        preview.addEventListener('keydown', event => {
            if (image.hidden) return;
            const direction = {ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1]}[event.key];
            if (!direction) return;
            event.preventDefault();
            const step = event.shiftKey ? 1 : 0.25;
            x.value = round(Math.max(-64, Math.min(64, Number(x.value) + direction[0] * step)));
            y.value = round(Math.max(-64, Math.min(64, Number(y.value) + direction[1] * step)));
            render();
        });
        file.addEventListener('change', () => {
            if (url) URL.revokeObjectURL(url);
            image.hidden = true;
            error.hidden = true;
            zoom.disabled = fit.disabled = center.disabled = true;
            drag = null;
            file.setCustomValidity('');
            const upload = file.files[0];
            if (!upload) return;
            if (upload.size > 10 * 1024 * 1024) {
                error.textContent = 'Choose an image up to 10 MB.';
                error.hidden = false;
                file.setCustomValidity(error.textContent);
                return;
            }
            url = URL.createObjectURL(upload);
            file.setCustomValidity('Please wait for the image preview to load.');
            image.src = url;
        });
        image.addEventListener('load', () => {
            const scale = Math.min(1 / 40, 64 / image.naturalWidth, 64 / image.naturalHeight);
            if (form.dataset.preserveFit !== 'true') {
                width.value = round(clamp(image.naturalWidth * scale));
                height.value = round(clamp(image.naturalHeight * scale));
                x.value = y.value = '0';
            }
            baseWidth = Number(width.value);
            baseHeight = Number(height.value);
            zoom.min = Math.max(0.25, 0.1 / baseWidth, 0.1 / baseHeight);
            zoom.max = Math.min(4, 64 / baseWidth, 64 / baseHeight);
            zoom.value = '1';
            zoom.disabled = fit.disabled = center.disabled = false;
            file.setCustomValidity('');
            form.dataset.preserveFit = 'false';
            image.hidden = false;
            render();
        });
        image.addEventListener('error', () => {
            error.textContent = 'This image could not be previewed. Choose another image.';
            error.hidden = false;
            file.setCustomValidity(error.textContent);
            image.hidden = true;
        });
        image.addEventListener('mousedown', event => {
            if (event.button !== 0) return;
            event.preventDefault();
            preview.focus({preventScroll: true});
            drag = {mouseX: event.clientX, mouseY: event.clientY, x: Number(x.value), y: Number(y.value),
                preview, scrollX: preview.scrollLeft, scrollY: preview.scrollTop};
        });
        function move(event) {
            if (!drag) return;
            x.value = round(Math.max(-64, Math.min(64, drag.x +
                (event.clientX - drag.mouseX + drag.preview.scrollLeft - drag.scrollX) / 16)));
            y.value = round(Math.max(-64, Math.min(64, drag.y +
                (event.clientY - drag.mouseY + drag.preview.scrollTop - drag.scrollY) / 16)));
            render();
        }
        const finish = () => { drag = null; };
        document.addEventListener('mousemove', move);
        document.addEventListener('mouseup', finish);
        window.addEventListener('blur', finish);
        dispose = () => {
            if (url) URL.revokeObjectURL(url);
            document.removeEventListener('mousemove', move);
            document.removeEventListener('mouseup', finish);
            window.removeEventListener('blur', finish);
        };
        render();
    }
    document.addEventListener('htmx:afterSwap', event => {
        if (event.detail.target.id === 'map-editor') setup();
    });
    document.addEventListener('click', event => {
        if (event.target.closest('[data-close-map-editor]')) dispose();
    });
    setup();
})();
