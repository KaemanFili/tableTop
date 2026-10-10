// Shared interpolation math. Callers own their animation frames, timing, and cleanup.
const movementMath = Object.freeze({
    progress(now, started, duration) {
        if (duration <= 0) return 1;
        return Math.min(1, Math.max(0, (now - started) / duration));
    },

    interpolate(from, to, progress) {
        return from + (to - from) * progress;
    },
});
