document.addEventListener('DOMContentLoaded', () => {
  const menuList = document.getElementById('menu-list');

  // Fetch Menu from Go API
  fetch('/api/menu')
    .then(res => res.json())
    .then(items => {
      menuList.innerHTML = ''; // Clear statically rendered items
      items.forEach(item => {
        const li = document.createElement('li');
        li.innerHTML = `
          <div class="menu-card hover:card">
            <figure class="card-banner img-holder" style="--width: 100; --height: 100;">
              <img src="${item.image}" width="100" height="100" loading="lazy" alt="${item.name}" class="img-cover">
            </figure>
            <div>
              <div class="title-wrapper">
                <h3 class="title-3">
                  <a href="#" class="card-title">${item.name}</a>
                </h3>
                ${item.badge ? `<span class="badge label-1">${item.badge}</span>` : ''}
                <span class="span title-2">$${item.price.toFixed(2)}</span>
              </div>
              <p class="card-text label-1">${item.description}</p>
            </div>
          </div>
        `;
        menuList.appendChild(li);
      });
    })
    .catch(err => {
      console.error('API Error, check if Go backend is running:', err);
    });

  // Antigravity AI UI Chatbot Logic
  const toggleBtn = document.getElementById('ai-chat-toggle');
  const closeBtn = document.getElementById('ai-chat-close');
  const chatBox = document.getElementById('ai-chat-box');
  const sendBtn = document.getElementById('ai-chat-send');
  const chatInput = document.getElementById('ai-chat-input');
  const messagesContainer = document.getElementById('ai-chat-messages');

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

    // Fetch from Go Chat API
    fetch('/api/chat', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({ message: text })
    })
      .then(res => res.json())
      .then(data => {
        setTimeout(() => {
          appendMessage(data.message, 'ai');
        }, 500); // Small delay for realistic feel
      })
      .catch(err => {
        setTimeout(() => {
          appendMessage('Sorry, the server is busy processing real-time analytics. Please try again.', 'ai');
        }, 500);
      });
  };

  sendBtn.addEventListener('click', sendMessage);
  chatInput.addEventListener('keypress', (e) => {
    if (e.key === 'Enter') sendMessage();
  });

  // Start Realtime SSE Event Listener
  const startSSE = () => {
    const evtSource = new EventSource('/api/events');
    evtSource.onmessage = function(event) {
      if (!event.data) return;
      try {
        const payload = JSON.parse(event.data);
        // Show Kitchen Activity or Reservations inside the AI Chat window
        appendMessage(`🔔 [Live Update]: ${payload.message}`, 'ai');
        
        // Pop up the chatbox briefly if it's hidden to notify the user
        if(!chatBox.classList.contains('active')) {
          chatBox.classList.add('active');
          setTimeout(() => chatBox.classList.remove('active'), 5000);
        }
      } catch (err) {
        console.error('Failed to parse SSE payload', err);
      }
    };
  };

  // Launch the SSE Listener
  startSSE();
});
