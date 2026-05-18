document.addEventListener('DOMContentLoaded', () => {
  const API_TOKEN = 'secret123';

  const requestJSON = async (url, options) => {
    const response = await fetch(url, options);
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      throw new Error(payload.error || `Request failed with ${response.status}`);
    }
    return payload;
  };

  const menuList = document.getElementById('menu-list');
  const menuSearch = document.getElementById('menu-search');
  const menuCategory = document.getElementById('menu-category');
  const menuSort = document.getElementById('menu-sort');
  const menuStats = document.getElementById('menu-stats');

  const createMenuItem = (item) => {
    const li = document.createElement('li');
    const card = document.createElement('div');
    card.className = 'menu-card hover:card';

    const figure = document.createElement('figure');
    figure.className = 'card-banner img-holder';
    figure.style.setProperty('--width', '100');
    figure.style.setProperty('--height', '100');

    const image = document.createElement('img');
    image.src = item.image;
    image.width = 100;
    image.height = 100;
    image.loading = 'lazy';
    image.alt = item.name;
    image.className = 'img-cover';
    figure.appendChild(image);

    const content = document.createElement('div');
    const titleWrapper = document.createElement('div');
    titleWrapper.className = 'title-wrapper';

    const heading = document.createElement('h3');
    heading.className = 'title-3';
    const link = document.createElement('a');
    link.href = '#reservation-form';
    link.className = 'card-title';
    link.textContent = item.name;
    heading.appendChild(link);
    titleWrapper.appendChild(heading);

    if (item.badge) {
      const badge = document.createElement('span');
      badge.className = 'badge label-1';
      badge.textContent = item.badge;
      titleWrapper.appendChild(badge);
    }

    const category = document.createElement('span');
    category.className = 'badge label-1 menu-category-badge';
    category.textContent = item.category;
    titleWrapper.appendChild(category);

    const price = document.createElement('span');
    price.className = 'span title-2';
    price.textContent = `$${Number(item.price).toFixed(2)}`;
    titleWrapper.appendChild(price);

    const description = document.createElement('p');
    description.className = 'card-text label-1';
    description.textContent = item.description;

    content.append(titleWrapper, description);
    card.append(figure, content);
    li.appendChild(card);
    return li;
  };

  const loadStats = async () => {
    if (!menuStats) return;
    try {
      const stats = await requestJSON('/api/stats');
      menuStats.textContent = `${stats.menuItems} dishes · ${stats.pendingBookings} pending bookings`;
    } catch (err) {
      menuStats.textContent = '';
      console.error('Stats API error:', err);
    }
  };

  const loadMenu = async () => {
    if (!menuList) return;
    const params = new URLSearchParams();
    if (menuSearch?.value.trim()) params.set('q', menuSearch.value.trim());
    if (menuCategory?.value) params.set('category', menuCategory.value);
    if (menuSort?.value) params.set('sort', menuSort.value);

    try {
      const items = await requestJSON(`/api/menu?${params.toString()}`);
      if (items.length === 0) {
        const empty = document.createElement('li');
        empty.className = 'menu-empty';
        empty.textContent = 'No dishes match that search.';
        menuList.replaceChildren(empty);
        return;
      }
      menuList.replaceChildren(...items.map(createMenuItem));
    } catch (err) {
      console.error('Menu API error:', err);
    }
  };

  const debounce = (fn, delay = 250) => {
    let timer;
    return (...args) => {
      window.clearTimeout(timer);
      timer = window.setTimeout(() => fn(...args), delay);
    };
  };

  const debouncedLoadMenu = debounce(loadMenu);
  menuSearch?.addEventListener('input', debouncedLoadMenu);
  menuCategory?.addEventListener('change', loadMenu);
  menuSort?.addEventListener('change', loadMenu);
  loadMenu();
  loadStats();

  const reservationForm = document.getElementById('reservation-form');
  const reservationStatus = document.getElementById('reservation-status');
  const refreshReservationsBtn = document.getElementById('refresh-reservations');
  const reservationList = document.getElementById('reservation-list');
  const reservationDate = reservationForm?.querySelector('input[name="reservation-date"]');
  if (reservationDate) {
    reservationDate.min = new Date().toISOString().slice(0, 10);
  }

  reservationForm?.addEventListener('submit', async (event) => {
    event.preventDefault();
    const formData = new FormData(reservationForm);
    const date = formData.get('reservation-date');
    const time = formData.get('reservation-time');
    const guests = Number.parseInt(String(formData.get('guests') || '1'), 10);

    reservationStatus.textContent = 'Sending your booking request...';
    reservationStatus.classList.remove('error');

    try {
      const created = await requestJSON(`/api/reservation?token=${API_TOKEN}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: formData.get('name'),
          phone: formData.get('phone'),
          guests,
          date: date && time ? new Date(`${date}T${time}:00`).toISOString() : ''
        })
      });

      reservationStatus.textContent = `Reservation ${created.id} received. We will confirm it shortly.`;
      reservationForm.reset();
      loadStats();
      loadReservations();
    } catch (err) {
      reservationStatus.textContent = err.message || 'Could not send the reservation. Please try again.';
      reservationStatus.classList.add('error');
    }
  });

  const renderReservations = (reservations) => {
    if (!reservationList) return;
    if (reservations.length === 0) {
      const empty = document.createElement('li');
      empty.textContent = 'No reservations yet.';
      reservationList.replaceChildren(empty);
      return;
    }

    reservationList.replaceChildren(...reservations.slice(0, 5).map((reservation) => {
      const item = document.createElement('li');
      const detail = document.createElement('span');
      const date = new Date(reservation.date);
      detail.textContent = `${reservation.name} · ${reservation.guests} guests · ${date.toLocaleString()} · ${reservation.status}`;
      item.appendChild(detail);

      if (reservation.status !== 'CANCELLED') {
        const cancel = document.createElement('button');
        cancel.type = 'button';
        cancel.textContent = 'Cancel';
        cancel.addEventListener('click', async () => {
          try {
            await requestJSON(`/api/reservations/${reservation.id}?token=${API_TOKEN}`, { method: 'DELETE' });
            await loadReservations();
            await loadStats();
          } catch (err) {
            reservationStatus.textContent = err.message || 'Could not cancel reservation.';
            reservationStatus.classList.add('error');
          }
        });
        item.appendChild(cancel);
      }

      return item;
    }));
  };

  async function loadReservations() {
    if (!reservationList) return;
    try {
      const reservations = await requestJSON(`/api/reservations?token=${API_TOKEN}`);
      renderReservations(reservations);
    } catch (err) {
      console.error('Reservations API error:', err);
    }
  }

  refreshReservationsBtn?.addEventListener('click', loadReservations);
  loadReservations();

  const toggleBtn = document.getElementById('ai-chat-toggle');
  const closeBtn = document.getElementById('ai-chat-close');
  const chatBox = document.getElementById('ai-chat-box');
  const sendBtn = document.getElementById('ai-chat-send');
  const chatInput = document.getElementById('ai-chat-input');
  const messagesContainer = document.getElementById('ai-chat-messages');

  if (!toggleBtn || !closeBtn || !chatBox || !sendBtn || !chatInput || !messagesContainer) {
    return;
  }

  toggleBtn.addEventListener('click', () => {
    chatBox.classList.toggle('active');
  });

  closeBtn.addEventListener('click', () => {
    chatBox.classList.remove('active');
  });

  const appendMessage = (text, sender) => {
    const msgDiv = document.createElement('div');
    msgDiv.classList.add('message', sender);
    msgDiv.textContent = text;
    messagesContainer.appendChild(msgDiv);
    messagesContainer.scrollTop = messagesContainer.scrollHeight;
  };

  const sendMessage = () => {
    const text = chatInput.value.trim();
    if (!text) return;

    appendMessage(text, 'user');
    chatInput.value = '';

    requestJSON('/api/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message: text })
    })
      .then((data) => {
        setTimeout(() => appendMessage(data.message, 'ai'), 350);
      })
      .catch(() => {
        setTimeout(() => {
          appendMessage('Sorry, the server is busy processing real-time analytics. Please try again.', 'ai');
        }, 350);
      });
  };

  sendBtn.addEventListener('click', sendMessage);
  chatInput.addEventListener('keydown', (event) => {
    if (event.key === 'Enter') sendMessage();
  });

  const startSSE = () => {
    const evtSource = new EventSource('/api/events?token=secret123');

    evtSource.onmessage = (event) => {
      if (!event.data) return;

      try {
        const payload = JSON.parse(event.data);
        appendMessage(`Live update: ${payload.message}`, 'ai');

        if (!chatBox.classList.contains('active')) {
          chatBox.classList.add('active');
          setTimeout(() => chatBox.classList.remove('active'), 5000);
        }
      } catch (err) {
        console.error('Failed to parse SSE payload', err);
      }
    };

    evtSource.onerror = () => console.warn('Live updates are temporarily unavailable.');
  };

  startSSE();
});
