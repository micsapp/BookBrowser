"""Create EPUB/PDF fixtures for test_offline.cjs in a disposable book directory."""
import pathlib
import shutil
import sys
import zipfile

if len(sys.argv) != 2:
    raise SystemExit("Usage: python3 scripts/create_offline_fixtures.py /tmp/offline-test/books")

destination = pathlib.Path(sys.argv[1])
destination.mkdir(parents=True, exist_ok=True)
epub = destination / "Offline Test.epub"
pdf = destination / "Offline PDF.pdf"
if epub.exists() or pdf.exists():
    raise SystemExit("Fixture files already exist; use a fresh directory.")

with zipfile.ZipFile(epub, "w", compression=zipfile.ZIP_DEFLATED) as archive:
    archive.writestr("mimetype", "application/epub+zip")
    archive.writestr("META-INF/container.xml", '<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container" version="1.0"><rootfiles><rootfile full-path="EPUB/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>')
    archive.writestr("EPUB/content.opf", '''<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Offline Test</dc:title><dc:creator>Test Author</dc:creator><dc:identifier id="bookid">offline-test-1</dc:identifier><dc:language>en</dc:language></metadata><manifest><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/><item id="one" href="one.xhtml" media-type="application/xhtml+xml"/><item id="two" href="two.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="one"/><itemref idref="two"/></spine></package>''')
    archive.writestr("EPUB/nav.xhtml", '<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="toc"><ol><li><a href="one.xhtml">One</a></li><li><a href="two.xhtml">Two</a></li></ol></nav></body></html>')
    for chapter in ["one", "two"]:
        paragraphs = "".join(
            f"<p>Paragraph {number}. Offline books should remain readable after the browser restarts. This paragraph supplies enough text to test pagination, saved reading positions, and local speech fallback.</p>"
            for number in range(60)
        )
        archive.writestr(f"EPUB/{chapter}.xhtml", f'<html xmlns="http://www.w3.org/1999/xhtml"><head><title>{chapter}</title></head><body><h1>Offline chapter {chapter}</h1>{paragraphs}</body></html>')

shutil.copyfile(pathlib.Path(__file__).resolve().parents[1] / "formats/pdf/pdf_test.pdf", pdf)
print(f"Created offline test fixtures in {destination}")
