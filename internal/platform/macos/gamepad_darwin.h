typedef struct {
    int id;
    char name[64];
    float axes[6]; // left x/y, right x/y (y up), left/right trigger 0–1
    int buttons;   // bits in qui.GamepadButton order
} QuiGamepad;

int quiGamepads(QuiGamepad *out, int max);
