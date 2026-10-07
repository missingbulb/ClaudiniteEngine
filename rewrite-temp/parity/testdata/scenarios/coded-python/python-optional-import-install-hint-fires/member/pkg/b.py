def load():
    try:
        import tensorflow as tf
    except ImportError as exc:
        raise
    return tf
