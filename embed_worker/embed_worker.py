#!/usr/bin/env python3
"""Thin fastembed wrapper: JSON {"texts": [...]} on stdin -> JSON {"vectors": [...]} on stdout."""
import json
import sys


def main() -> None:
    payload = json.load(sys.stdin)
    texts = payload["texts"]

    from fastembed import TextEmbedding

    model = TextEmbedding(model_name="BAAI/bge-small-en-v1.5", cuda=True)
    vectors = [[float(x) for x in vector] for vector in model.embed(texts)]
    json.dump({"vectors": vectors}, sys.stdout)


if __name__ == "__main__":
    main()
