document.addEventListener('DOMContentLoaded', () => {
  const requestJSON = async (url, options) => {
    const response = await fetch(url, options);
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      throw new Error(payload.error || `Request failed with ${response.status}`);
    }
    return payload;
  };

  const menuList = document.getElementById('menu-list');

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

  if (menuList) {
    requestJSON('/api/menu')
      .then((items) => menuList.replaceChildren(...items.map(createMenuItem)))
      .catch((err) => console.error('Menu API error:', err));
  }

  const reservationForm = document.getElementById('reservation-form');
  const reservationStatus = document.getElementById('reservation-status');
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
      const created = await requestJSON('/api/reservation?token=secret123', {
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
    } catch (err) {
      reservationStatus.textContent = err.message || 'Could not send the reservation. Please try again.';
      reservationStatus.classList.add('error');
    }
  });

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
