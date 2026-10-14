// Adapter for fixed-result dice animations. The roller owns requests and results.
const diceAnimation = Object.freeze({
    create(layer, status) {
        const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)');
        let renderer;
        let fadeTimer;
        let queue = Promise.resolve();
        let queued = 0;

        async function withTimeout(promise, message) {
            let timer;
            try {
                return await Promise.race([promise, new Promise((_, reject) => {
                    timer = setTimeout(() => reject(new Error(message)), 15000);
                })]);
            } finally {
                clearTimeout(timer);
            }
        }

        function load() {
            if (renderer) return renderer;
            renderer = (async () => {
                if (reducedMotion.matches) return null;
                const canvas = document.createElement('canvas');
                const gl = canvas.getContext('webgl');
                if (!gl) {
                    status.textContent = '3D animations unavailable. Rolls still work.';
                    return null;
                }
                gl.getExtension('WEBGL_lose_context')?.loseContext();
                status.textContent = 'Loading dice…';
                try {
                    const {default: DiceBox} = await import('/static/vendor/dice-box-threejs-0.0.12/dice-box-threejs.es.js');
                    const box = new DiceBox(`#${layer.id}`, {
                        sounds: false,
                        shadows: false,
                        theme_colorset: 'white',
                        theme_texture: 'none',
                        theme_material: 'plastic',
                        baseScale: 50,
                        strength: 1,
                    });
                    // Upstream keeps the old d4 result after changing its faces.
                    const swapD4 = box.swapDiceFace_D4.bind(box);
                    box.swapDiceFace_D4 = (die, value) => {
                        swapD4(die, value);
                        die.result = [];
                    };
                    await withTimeout(box.initialize(), 'Dice animation could not load');
                    status.textContent = '';
                    return box;
                } catch (error) {
                    console.warn('Could not initialize dice animation:', error);
                    status.textContent = '3D animations unavailable. Rolls still work.';
                    renderer = null;
                    layer.replaceChildren();
                    return null;
                }
            })();
            return renderer;
        }

        async function animate(message) {
            const count = message.groups.reduce((total, group) => total + group.values.length * (group.sides === 100 ? 2 : 1), 0);
            if (count > 20) {
                clearTimeout(fadeTimer);
                layer.style.opacity = '0';
                status.textContent = '3D animation skipped for rolls over 20 dice. All results are shown.';
                return;
            }
            const box = await load();
            if (!box || reducedMotion.matches) return;
            status.textContent = '';
            clearTimeout(fadeTimer);
            layer.style.opacity = '1';
            // The engine combines repeated die types; keep their fixed values in
            // that same order while preserving separate modifier groups in the UI.
            const byType = new Map();
            const addValue = (sides, value) => {
                if (!byType.has(sides)) byType.set(sides, []);
                byType.get(sides).push(value);
            };
            message.groups.forEach(group => group.values.forEach(value => {
                if (group.sides === 100) {
                    // Percentile rolls use a tens die and a units die. Their zero
                    // faces are represented by 100 and 10 in the engine.
                    addValue(100, Math.floor((value % 100) / 10) * 10 || 100);
                    addValue(10, value % 10 || 10);
                } else addValue(group.sides, value);
            }));
            const notation = [...byType].map(([sides, values]) => `${values.length}d${sides}`).join('+');
            const values = [...byType.values()].flat().join(',');
            try {
                await withTimeout(box.roll(`${notation}@${values}`), 'Dice animation did not finish');
            } catch (error) {
                console.warn('Could not animate shared dice roll:', error);
                box.clearDice();
                layer.style.opacity = '0';
            } finally {
                fadeTimer = setTimeout(() => { layer.style.opacity = '0'; }, 2500);
            }
        }

        function play(message) {
            // Results are already displayed. Keep rapid shared rolls from building
            // an unbounded physics-animation backlog on every connected browser.
            if (queued >= 3) return Promise.resolve();
            queued++;
            queue = queue.then(() => animate(message)).catch(error => {
                console.warn('Could not display dice animation:', error);
            }).finally(() => { queued--; });
            return queue;
        }
        return {load, play};
    },
});
