from airflow.plugins_manager import AirflowPlugin
from airflow.listeners import hookimpl
import requests
import os
import logging

# Use Airflow's logging system
logger = logging.getLogger("airflow.plugin.metrics")

class MetricsListener:
    def __init__(self):
        logger.info("metrics listener initialized")

    # Task Instance Lifecycle Hooks
    @hookimpl
    def on_task_instance_running(self, task_instance, **kwargs):
        """Called when a task instance starts running."""
        logger.debug(f"Task Instance Running triggered for {task_instance.task_id}")
        start_time = task_instance.start_date
        if start_time:
            logger.info(
                f"Task {task_instance.task_id} in DAG {task_instance.dag_id} "
                f"started at {start_time.isoformat()}"
            )
        else:
            logger.warning(
                f"Task {task_instance.task_id} in DAG {task_instance.dag_id} "
                "started but start_date unavailable"
            )
        self._send_metrics(
            event_type="task_running",
            dag_id=task_instance.dag_id,
            task_id=task_instance.task_id,
            execution_time=task_instance.execution_date.isoformat(),
            start_time=start_time.isoformat() if start_time else None
        )

    @hookimpl
    def on_task_instance_success(self, task_instance, **kwargs):
        """Called when a task instance succeeds."""
        logger.debug(f"Task Instance Success triggered for {task_instance.task_id}")
        logger.info(f"Task {task_instance.task_id} in DAG {task_instance.dag_id} succeeded")
        self._send_metrics(
            event_type="task_success",
            dag_id=task_instance.dag_id,
            task_id=task_instance.task_id,
            execution_time=task_instance.execution_date.isoformat(),
            start_time=task_instance.start_date.isoformat() if task_instance.start_date else None,
            duration=task_instance.duration if task_instance.duration is not None else 0
        )

    @hookimpl
    def on_task_instance_failed(self, task_instance, **kwargs):
        """Called when a task instance fails."""
        logger.debug(f"Task Instance Failed triggered for {task_instance.task_id}")
        logger.info(f"Task {task_instance.task_id} in DAG {task_instance.dag_id} failed")
        self._send_metrics(
            event_type="task_failed",
            dag_id=task_instance.dag_id,
            task_id=task_instance.task_id,
            execution_time=task_instance.execution_date.isoformat(),
            start_time=task_instance.start_date.isoformat() if task_instance.start_date else None,
            duration=task_instance.duration if task_instance.duration is not None else 0
        )

    # DAG Run Lifecycle Hooks
    @hookimpl
    def on_dag_run_running(self, dag_run, **kwargs):
        """Called when a DAG run starts."""
        logger.debug(f"DAG Run Running triggered for DAG {dag_run.dag_id}")
        start_time = dag_run.start_date
        logger.info(
            f"DAG Run for {dag_run.dag_id} started at {start_time.isoformat()}"
            if start_time else f"DAG Run for {dag_run.dag_id} started, no start_date yet"
        )
        self._send_metrics(
            event_type="dag_running",
            dag_id=dag_run.dag_id,
            execution_time=dag_run.execution_date.isoformat(),
            start_time=start_time.isoformat() if start_time else None
        )

    @hookimpl
    def on_dag_run_success(self, dag_run, **kwargs):
        """Called when a DAG run succeeds."""
        logger.debug(f"DAG Run Success triggered for DAG {dag_run.dag_id}")
        logger.info(f"DAG Run for {dag_run.dag_id} succeeded")
        self._send_metrics(
            event_type="dag_success",
            dag_id=dag_run.dag_id,
            execution_time=dag_run.execution_date.isoformat(),
            start_time=dag_run.start_date.isoformat() if dag_run.start_date else None
        )

    @hookimpl
    def on_dag_run_failed(self, dag_run, **kwargs):
        """Called when a DAG run fails."""
        logger.debug(f"DAG Run Failed triggered for DAG {dag_run.dag_id}")
        logger.info(f"DAG Run for {dag_run.dag_id} failed")
        self._send_metrics(
            event_type="dag_failed",
            dag_id=dag_run.dag_id,
            execution_time=dag_run.execution_date.isoformat(),
            start_time=dag_run.start_date.isoformat() if dag_run.start_date else None
        )

    # Executor-Related Hooks
    @hookimpl
    def before_executor_task_run(self, task_instance, **kwargs):
        """Called before a task is sent to the executor."""
        logger.debug(f"Before Executor Task Run triggered for {task_instance.task_id}")
        logger.info(f"Task {task_instance.task_id} in DAG {task_instance.dag_id} queued for execution")
        self._send_metrics(
            event_type="task_queued",
            dag_id=task_instance.dag_id,
            task_id=task_instance.task_id,
            execution_time=task_instance.execution_date.isoformat()
        )

    @hookimpl
    def on_executor_task_complete(self, task_instance, **kwargs):
        """Called after a task completes in the executor."""
        logger.debug(f"Executor Task Complete triggered for {task_instance.task_id}")
        logger.info(f"Task {task_instance.task_id} in DAG {task_instance.dag_id} completed in executor")
        self._send_metrics(
            event_type="task_executor_complete",
            dag_id=task_instance.dag_id,
            task_id=task_instance.task_id,
            execution_time=task_instance.execution_date.isoformat(),
            start_time=task_instance.start_date.isoformat() if task_instance.start_date else None,
            duration=task_instance.duration if task_instance.duration is not None else 0
        )

    def _send_metrics(self, event_type, dag_id, execution_time, task_id=None, start_time=None, duration=None):
        """Send metrics to the Golang service."""
        logger.debug(f"Sending metrics for event {event_type}, DAG {dag_id}, Task {task_id or 'N/A'}")
        metrics = {
            "event_type": event_type,
            "dag_id": dag_id,
            "execution_time": execution_time,
            "task_id": task_id if task_id else None,
            "start_time": start_time,
            "duration": duration if duration is not None else None
        }

        metrics_url = os.getenv("METRICS_SERVICE_URL")
        if not metrics_url:
            logger.error("METRICS_SERVICE_URL not set")
            logger.info(f"setting URL to default http://airflow-observer:8000/metrics/events")
            metrics_url = "http://airflow-observer:8000/metrics/events"

        try:
            response = requests.post(metrics_url, json=metrics, timeout=5)
            response.raise_for_status()
            logger.info(f"Metrics sent for event {event_type}, DAG {dag_id}, Task {task_id or 'N/A'}")
        except Exception as e:
            logger.error(f"Failed to send metrics for event {event_type}, DAG {dag_id}, Task {task_id or 'N/A'}: {e}")

class MetricsPlugin(AirflowPlugin):
    name = "metrics_collector"
    listeners = [MetricsListener()]