from bs4 import BeautifulSoup
import requests
import os
import re
from urllib.parse import urljoin, urlparse

# Maximum recursion depth to prevent infinite loops
maxRekrusion = 100

# Track visited URLs
visitedUrls = set()

def get_domain_folder(url):
    """Creates a base folder named after the domain for each URL."""
    parsed_url = urlparse(url)
    domain = parsed_url.netloc.replace(".", "_")
    base_folder = os.path.join("data", domain)
    os.makedirs(base_folder, exist_ok=True)
    return base_folder

def download_image(img_url, folder_path):
    headers = {
        'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.88 Safari/537.36'
    }
    try:
        response = requests.get(img_url, headers=headers, stream=True)
        response.raise_for_status()
        
        # Sanitize and create image filename
        img_name = os.path.basename(img_url.split("?")[0])
        img_name = re.sub(r"[^\w\.]", "_", img_name)
        img_path = os.path.join(folder_path, img_name)

        # Save image
        with open(img_path, 'wb') as file:
            for chunk in response.iter_content(1024):
                file.write(chunk)
        print(f"Downloaded image: {img_name}")
    except requests.RequestException as e:
        print(f"Failed to download image {img_url}: {e}")


def save_text(content, filename):
    """Saves the text content to a file."""
    with open(filename, 'w', encoding='utf-8') as file:
        file.write(content)

def scrape(url, depth=0, base_folder=None):
    """Recursively scrape URLs for text and images, saving content in unique folders."""
    if url in visitedUrls or depth >= maxRekrusion:
        return

    print(f"Scraping: {url} (depth {depth})")
    visitedUrls.add(url)

    # Create a unique folder for each domain if not provided
    if base_folder is None:
        base_folder = get_domain_folder(url)

    # Set paths for text and images
    img_folder = os.path.join(base_folder, "img")
    text_folder = os.path.join(base_folder, "text")
    os.makedirs(img_folder, exist_ok=True)
    os.makedirs(text_folder, exist_ok=True)

    try:
        response = requests.get(url)
        response.raise_for_status()
    except requests.RequestException as e:
        print(f"Failed to retrieve {url}: {e}")
        return

    # Parse page content
    soup = BeautifulSoup(response.text, 'html.parser')

    # Extract and save all text content from <p>, <h1>, <h2>, etc.
    page_text = "\n".join(element.get_text(strip=True) for element in soup.find_all(['p', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'li']))
    page_filename = os.path.join(text_folder, f"page_{depth}.txt")
    save_text(page_text, page_filename)

    # Extract and download images
    img_urls = [urljoin(url, img_tag['src']) for img_tag in soup.find_all("img", src=True)]
    for img_url in img_urls:
        download_image(img_url, img_folder)

    # Find all new URLs to crawl, avoiding repetition
    new_urls = [urljoin(url, a['href']) for a in soup.find_all("a", href=True)]
    for link in new_urls:
        if link not in visitedUrls:
            scrape(link, depth + 1, base_folder)  # Recursively scrape the new link

# Start the recursive scraping
url = input("Give a URL to Recursive Scrape: ")
scrape(url)
