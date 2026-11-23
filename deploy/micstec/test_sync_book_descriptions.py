import json
import os
import stat
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import sync_book_descriptions as sync


def make_entry(key, text="text", retrieved_at="2026-11-21T00:00:00Z", title="Title"):
    return {
        "book_hash": key + "0" * (40 - len(key)),
        "title": title,
        "author": "Author",
        "text": text,
        "method": "manual",
        "language": "zh-Hans",
        "retrieved_at": retrieved_at,
        "reviewed": True,
        "sources": [{"label": "Test"}],
    }


def make_document(entries):
    return {
        "version": 1,
        "updated_at": "2026-11-21T00:00:00Z",
        "descriptions": {key: make_entry(key, **values) for key, values in entries.items()},
    }


class MergeTests(unittest.TestCase):
    def test_union_preserves_local_and_remote_only_entries(self):
        local = make_document(
            {
                "aaaaaaaaaa": {"text": "local a"},
                "bbbbbbbbbb": {"text": "shared b"},
            }
        )
        remote = make_document(
            {
                "bbbbbbbbbb": {"text": "shared b"},
                "cccccccccc": {"text": "remote c"},
            }
        )
        merged, stats = sync.merge_documents(local, remote)
        self.assertEqual(stats["local"], 2)
        self.assertEqual(stats["remote"], 2)
        self.assertEqual(stats["merged"], 3)
        self.assertEqual(stats["local_only"], 1)
        self.assertEqual(stats["remote_only"], 1)
        self.assertEqual(stats["conflicts"], [])
        self.assertIn("aaaaaaaaaa", merged["descriptions"])
        self.assertIn("bbbbbbbbbb", merged["descriptions"])
        self.assertIn("cccccccccc", merged["descriptions"])
        self.assertTrue(stats["changed_local"])
        self.assertTrue(stats["changed_remote"])

    def test_remote_wins_when_retrieved_at_is_newer(self):
        local = make_document({"aaaaaaaaaa": {"text": "old"}})
        remote = make_document(
            {"aaaaaaaaaa": {"text": "new", "retrieved_at": "2026-11-22T00:00:00Z"}}
        )
        merged, stats = sync.merge_documents(local, remote)
        self.assertEqual(merged["descriptions"]["aaaaaaaaaa"]["text"], "new")
        self.assertEqual(len(stats["conflicts"]), 1)
        self.assertEqual(stats["conflicts"][0]["chosen"], "remote")
        self.assertTrue(stats["changed_local"])
        self.assertFalse(stats["changed_remote"])

    def test_local_wins_when_retrieved_at_is_tied_or_missing(self):
        local = make_document({"aaaaaaaaaa": {"text": "local"}})
        remote = make_document({"aaaaaaaaaa": {"text": "remote"}})
        merged, stats = sync.merge_documents(local, remote)
        self.assertEqual(merged["descriptions"]["aaaaaaaaaa"]["text"], "local")
        self.assertEqual(len(stats["conflicts"]), 1)
        self.assertEqual(stats["conflicts"][0]["chosen"], "local")
        self.assertFalse(stats["changed_local"])
        self.assertTrue(stats["changed_remote"])

    def test_invalid_version_is_rejected(self):
        document = make_document({"aaaaaaaaaa": {}})
        document["version"] = 2
        with self.assertRaises(sync.SyncError):
            sync.validate_document(document, "test")

    def test_invalid_book_hash_is_rejected(self):
        document = make_document({"aaaaaaaaaa": {}})
        document["descriptions"]["aaaaaaaaaa"]["book_hash"] = "z" * 40
        with self.assertRaises(sync.SyncError):
            sync.validate_document(document, "test")

    def test_duplicate_book_hash_is_rejected(self):
        document = make_document({"aaaaaaaaa": {}, "aaaaaaaaaa": {}})
        document["descriptions"]["aaaaaaaaaa"]["book_hash"] = "a" * 40
        document["descriptions"]["aaaaaaaaa"]["book_hash"] = "a" * 40
        with self.assertRaises(sync.SyncError):
            sync.validate_document(document, "test")


class FileTests(unittest.TestCase):
    def test_write_atomic_sets_private_mode(self):
        with tempfile.TemporaryDirectory() as directory:
            path = os.path.join(directory, "book-descriptions.json")
            data = json.dumps(make_document({"aaaaaaaaaa": {}}), ensure_ascii=False) + "\n"
            sync.write_atomic(path, data)
            with open(path, "r", encoding="utf-8") as handle:
                self.assertEqual(json.load(handle)["version"], 1)
            self.assertEqual(stat.S_IMODE(os.stat(path).st_mode), 0o600)

    def test_backup_local_copies_contents(self):
        with tempfile.TemporaryDirectory() as directory:
            path = os.path.join(directory, "book-descriptions.json")
            with open(path, "w", encoding="utf-8") as handle:
                handle.write("test\n")
            backup = sync.backup_local(path)
            self.assertIsNotNone(backup)
            with open(backup, "r", encoding="utf-8") as handle:
                self.assertEqual(handle.read(), "test\n")


if __name__ == "__main__":
    unittest.main()
