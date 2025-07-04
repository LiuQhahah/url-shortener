document.getElementById('shortenForm').addEventListener('submit', async function(event) {
    event.preventDefault();

    const form = event.target;
    const formData = new FormData(form);
    const url = formData.get('url');

    try {
        const response = await fetch(form.action, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/x-www-form-urlencoded'
            },
            body: `url=${encodeURIComponent(url)}`
        });

        if (response.ok) {
            const data = await response.json();
            const shortUrl = data.short_url;

            const shortUrlLink = document.getElementById('shortUrlLink');
            shortUrlLink.href = shortUrl;
            shortUrlLink.textContent = shortUrl;

            document.getElementById('result').style.display = 'block';
        } else {
            const errorText = await response.text();
            alert('Error: ' + errorText);
        }
    } catch (error) {
        console.error('Fetch error:', error);
        alert('An error occurred while shortening the URL.');
    }
});

document.getElementById('copyButton').addEventListener('click', function() {
    const shortUrlLink = document.getElementById('shortUrlLink');
    const textToCopy = shortUrlLink.textContent;

    navigator.clipboard.writeText(textToCopy).then(function() {
        alert('Copied to clipboard!');
    }).catch(function(err) {
        console.error('Could not copy text: ', err);
        alert('Failed to copy URL.');
    });
});