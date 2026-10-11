// Reusable notification lifecycle; callers supply the title and content.
const notificationUI = Object.freeze({
    create(container, template, {limit = 5, duration = 30000} = {}) {
        const timers = new Map();
        function dismiss(card) {
            clearTimeout(timers.get(card));
            timers.delete(card);
            card.remove();
        }
        container.addEventListener('click', event => {
            const button = event.target.closest('[data-dismiss-notification]');
            if (button) dismiss(button.closest('article'));
        });
        function show(title, content) {
            const card = template.content.firstElementChild.cloneNode(true);
            card.querySelector('[data-notification-title]').textContent = title;
            card.querySelector('[data-notification-content]').append(content);
            container.append(card);
            timers.set(card, setTimeout(() => dismiss(card), duration));
            while (container.children.length > limit) dismiss(container.firstElementChild);
        }
        return {show};
    },
});
