def load():
    try:
        import tensorflow as tf
    except ImportError as exc:
        raise ImportError("pip install pkg[yamnet]") from exc
    return tf
