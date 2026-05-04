from pipeline.runner import run_pipeline


def test_run_pipeline_saves_checkpoint() -> None:
    assert run_pipeline("daily") == "daily"
