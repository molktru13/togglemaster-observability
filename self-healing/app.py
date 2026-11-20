import logging
import os
import threading
import time
from datetime import datetime, timezone

from flask import Flask, jsonify, request

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(levelname)s - %(message)s",
)
log = logging.getLogger(__name__)

ALLOWED_SERVICES = {
    "auth-service": "togglemaster-auth",
    "flag-service": "togglemaster-flags",
    "targeting-service": "togglemaster-targeting",
    "evaluation-service": "togglemaster-evaluation",
    "analytics-service": "togglemaster-analytics",
}

COOLDOWN_SECONDS = int(os.getenv("SELF_HEALING_COOLDOWN_SECONDS", "60"))
DEFAULT_TARGET_SERVICE = os.getenv("DEFAULT_TARGET_SERVICE", "auth-service")

_cooldown_lock = threading.Lock()
_last_restart = {}

app = Flask(__name__)


def restart_deployment(service_name, namespace):
    """Executa equivalente ao kubectl rollout restart deployment/<service> via API Kubernetes."""
    from kubernetes import client, config

    config.load_incluster_config()
    apps_v1 = client.AppsV1Api()
    restarted_at = datetime.now(timezone.utc).isoformat()
    body = {
        "spec": {
            "template": {
                "metadata": {
                    "annotations": {
                        "kubectl.kubernetes.io/restartedAt": restarted_at,
                    }
                }
            }
        }
    }
    apps_v1.patch_namespaced_deployment(name=service_name, namespace=namespace, body=body)
    return restarted_at


def _parse_grafana_payload(payload):
    """Extrai status, nome do alerta e service_name alvo do payload de webhook do Grafana."""
    alerts = payload.get("alerts")
    labels = dict(payload.get("commonLabels") or {})
    status = payload.get("status", "")

    if isinstance(alerts, list) and alerts:
        firing = [item for item in alerts if item.get("status") == "firing"]
        selected = firing[0] if firing else alerts[0]
        status = selected.get("status", status)
        labels.update(selected.get("labels") or {})

    alert_name = labels.get("alertname") or payload.get("title") or "ToggleMasterAlert"
    service_name = labels.get("service_name") or labels.get("deployment") or DEFAULT_TARGET_SERVICE
    return status, alert_name, service_name


@app.route("/health", methods=["GET"])
def health():
    return jsonify({"status": "ok"})


@app.route("/webhook", methods=["POST"])
def webhook():
    """Recebe webhook do Grafana Alerting e executa Self-Healing em alertas FIRING."""
    payload = request.get_json(silent=True) or {}
    status, alert_name, service_name = _parse_grafana_payload(payload)

    if status != "firing":
        log.info(f"[SELF-HEALING] Evento ignorado (status='{status}', alerta='{alert_name}').")
        return jsonify({"status": "ignored", "reason": "not firing", "alert_status": status}), 200

    namespace = ALLOWED_SERVICES.get(service_name)
    if not namespace:
        log.warning(f"[SELF-HEALING] Servico nao autorizado na allow-list: '{service_name}'.")
        return jsonify({"error": f"Servico '{service_name}' nao permitido"}), 400

    now = time.monotonic()
    with _cooldown_lock:
        last_ts = _last_restart.get(service_name)
        if last_ts is not None and (now - last_ts) < COOLDOWN_SECONDS:
            remaining = int(COOLDOWN_SECONDS - (now - last_ts))
            log.info(
                f"[SELF-HEALING] Cooldown ativo para '{service_name}' "
                f"({remaining}s restantes). Reinicio duplicado ignorado."
            )
            return jsonify({"status": "skipped", "reason": "cooldown", "service": service_name}), 200
        _last_restart[service_name] = now

    log.warning(
        f"[SELF-HEALING] Alerta FIRING recebido ({alert_name}). "
        f"Executando rollout restart em deployment/{service_name} no namespace {namespace}..."
    )
    try:
        restarted_at = restart_deployment(service_name, namespace)
    except Exception as exc:
        with _cooldown_lock:
            _last_restart.pop(service_name, None)
        log.error(f"[SELF-HEALING] Falha ao reiniciar deployment/{service_name}: {exc}")
        return jsonify({"error": "Falha ao executar rollout restart", "details": str(exc)}), 500

    log.info(
        f"[SELF-HEALING] Rollout restart concluido com sucesso em "
        f"deployment/{service_name} (namespace={namespace}, restartedAt={restarted_at})."
    )
    return jsonify(
        {
            "status": "restarted",
            "service": service_name,
            "namespace": namespace,
            "restarted_at": restarted_at,
        }
    ), 200


if __name__ == "__main__":
    port = int(os.getenv("PORT", "8080"))
    app.run(host="0.0.0.0", port=port, debug=False)  # nosec B104
