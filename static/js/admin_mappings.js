const mappingsTableBody = document.querySelector('#mappingsTable tbody');
const paginationControls = document.getElementById('paginationControls');
const totalUrlsSpan = document.getElementById('totalUrls');

let currentPage = 1;
const pageSize = 100; // Default page size

async function fetchMappings(page) {
    try {
        const response = await fetch(`/mappings-api?page=${page}&pageSize=${pageSize}`);
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        const data = await response.json();
        
        totalUrlsSpan.textContent = data.total_count;
        renderTable(data.mappings);
        renderPagination(data.page, data.total_pages);

    } catch (error) {
        console.error('Error fetching mappings:', error);
        mappingsTableBody.innerHTML = '<tr><td colspan="5">Error loading mappings.</td></tr>';
        paginationControls.innerHTML = '';
    }
}

function renderTable(mappings) {
    mappingsTableBody.innerHTML = ''; // Clear existing rows
    if (mappings && mappings.length > 0) {
        mappings.forEach(mapping => {
            const row = mappingsTableBody.insertRow();
            row.insertCell().textContent = mapping.short_url;
            const originalUrlCell = row.insertCell();
            const originalUrlLink = document.createElement('a');
            originalUrlLink.href = mapping.original_url;
            originalUrlLink.textContent = mapping.original_url;
            originalUrlLink.target = '_blank';
            originalUrlCell.appendChild(originalUrlLink);
            row.insertCell().textContent = mapping.count;
            row.insertCell().textContent = mapping.device || 'N/A';
            row.insertCell().textContent = mapping.os || 'N/A';
        });
    } else {
        mappingsTableBody.innerHTML = '<tr><td colspan="5">No mappings found.</td></tr>';
    }
}

function renderPagination(currentPage, totalPages) {
    paginationControls.innerHTML = ''; // Clear existing controls

    // Previous button
    if (currentPage > 1) {
        const prevLink = document.createElement('a');
        prevLink.href = '#';
        prevLink.textContent = 'Previous';
        prevLink.addEventListener('click', (e) => {
            e.preventDefault();
            currentPage--;
            fetchMappings(currentPage);
        });
        paginationControls.appendChild(prevLink);
    } else {
        const span = document.createElement('span');
        span.className = 'disabled';
        span.textContent = 'Previous';
        paginationControls.appendChild(span);
    }

    // Page numbers
    const startPage = Math.max(1, currentPage - 2);
    const endPage = Math.min(totalPages, currentPage + 2);

    for (let i = startPage; i <= endPage; i++) {
        if (i === currentPage) {
            const span = document.createElement('span');
            span.className = 'current';
            span.textContent = i;
            paginationControls.appendChild(span);
        } else {
            const pageLink = document.createElement('a');
            pageLink.href = '#';
            pageLink.textContent = i;
            pageLink.addEventListener('click', (e) => {
                e.preventDefault();
                currentPage = i;
                fetchMappings(currentPage);
            });
            paginationControls.appendChild(pageLink);
        }
    }

    // Next button
    if (currentPage < totalPages) {
        const nextLink = document.createElement('a');
        nextLink.href = '#';
        nextLink.textContent = 'Next';
        nextLink.addEventListener('click', (e) => {
            e.preventDefault();
            currentPage++;
            fetchMappings(currentPage);
        });
        paginationControls.appendChild(nextLink);
    } else {
        const span = document.createElement('span');
        span.className = 'disabled';
        span.textContent = 'Next';
        paginationControls.appendChild(span);
    }
}

// Initial fetch
fetchMappings(currentPage);