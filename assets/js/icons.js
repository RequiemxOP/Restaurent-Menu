(() => {
  const iconMap = {
    'location-outline': 'map-pin',
    'time-outline': 'clock',
    'call-outline': 'phone',
    'mail-outline': 'mail',
    'close-outline': 'x',
    'chevron-back': 'chevron-left',
    'chevron-forward': 'chevron-right',
    'chevron-up': 'chevron-up',
    'chevron-down': 'chevron-down',
    'person-outline': 'users',
    'calendar-clear-outline': 'calendar-days',
    'chatbubbles-outline': 'messages-square',
    'send-outline': 'send'
  };

  window.renderLucideIcons = () => {
    if (!window.lucide) return;

    document.querySelectorAll('ion-icon[name]').forEach((icon) => {
      const lucideName = iconMap[icon.getAttribute('name')] || 'circle';
      const replacement = document.createElement('i');
      replacement.setAttribute('data-lucide', lucideName);
      replacement.setAttribute('aria-hidden', icon.getAttribute('aria-hidden') || 'true');
      replacement.className = icon.className;
      icon.replaceWith(replacement);
    });

    window.lucide.createIcons({
      attrs: {
        'aria-hidden': 'true'
      }
    });
  };

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', window.renderLucideIcons, { once: true });
  } else {
    window.renderLucideIcons();
  }
})();
