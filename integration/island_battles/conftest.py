"""Load the host SDK while using the shared lobby acceptance environment."""
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "clients/island-battles-python"))
