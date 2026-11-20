import os
import unittest
from unittest.mock import patch

os.environ["TESTING"] = "true"

import app as self_healing_app  # noqa: E402


class SelfHealingTestCase(unittest.TestCase):
    def setUp(self):
        self.client = self_healing_app.app.test_client()
        self_healing_app._last_restart.clear()

    def test_health_endpoint(self):
        response = self.client.get("/health")
        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.get_json(), {"status": "ok"})

    @patch("app.restart_deployment", return_value="2026-11-20T12:00:00+00:00")
    def test_firing_webhook_triggers_restart_and_cooldown(self, mock_restart):
        payload = {
            "status": "firing",
            "alerts": [
                {
                    "status": "firing",
                    "labels": {
                        "alertname": "ToggleMasterHigh5xx",
                        "service_name": "auth-service",
                    },
                }
            ],
        }
        first = self.client.post("/webhook", json=payload)
        self.assertEqual(first.status_code, 200)
        self.assertEqual(first.get_json()["status"], "restarted")
        mock_restart.assert_called_once_with("auth-service", "togglemaster-auth")

        # Chamada imediata seguinte deve ser controlada pelo cooldown
        second = self.client.post("/webhook", json=payload)
        self.assertEqual(second.status_code, 200)
        self.assertEqual(second.get_json()["status"], "skipped")
        self.assertEqual(second.get_json()["reason"], "cooldown")
        mock_restart.assert_called_once()

    @patch("app.restart_deployment")
    def test_resolved_webhook_is_ignored(self, mock_restart):
        payload = {
            "status": "resolved",
            "alerts": [
                {
                    "status": "resolved",
                    "labels": {"alertname": "ToggleMasterHigh5xx", "service_name": "auth-service"},
                }
            ],
        }
        response = self.client.post("/webhook", json=payload)
        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.get_json()["status"], "ignored")
        mock_restart.assert_not_called()

    @patch("app.restart_deployment")
    def test_unlisted_service_returns_400(self, mock_restart):
        payload = {
            "status": "firing",
            "commonLabels": {"service_name": "kube-apiserver"},
        }
        response = self.client.post("/webhook", json=payload)
        self.assertEqual(response.status_code, 400)
        mock_restart.assert_not_called()


if __name__ == "__main__":
    unittest.main()
