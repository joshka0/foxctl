from pipeline.checkpoints import save_checkpoint


def run_pipeline(name: str) -> str:
    save_checkpoint(name)
    return name
