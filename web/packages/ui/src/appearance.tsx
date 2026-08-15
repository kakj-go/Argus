import { Languages, Moon, Sun } from "lucide-react";
import { Button } from "./button";
import { useLocale, useUiText } from "./locale";
import { Dropdown, Tooltip } from "./primitives";
import { useTheme } from "./theme";

export function AppearanceControls() {
  const text = useUiText();
  const { locale, setLocale } = useLocale();
  const { resolvedTheme, toggleTheme } = useTheme();
  const themeLabel =
    resolvedTheme === "dark"
      ? text("切换到浅色模式", "Switch to light mode")
      : text("切换到深色模式", "Switch to dark mode");

  return (
    <div className="argus-appearance-controls">
      <Tooltip content={themeLabel}>
        <Button
          aria-label={themeLabel}
          onClick={toggleTheme}
          size="icon"
          variant="ghost"
        >
          {resolvedTheme === "dark" ? <Sun size={16} /> : <Moon size={16} />}
        </Button>
      </Tooltip>
      <Dropdown
        items={[
          {
            label: `${locale === "zh-CN" ? "✓ " : ""}中文`,
            onSelect: () => setLocale("zh-CN"),
          },
          {
            label: `${locale === "en-US" ? "✓ " : ""}English`,
            onSelect: () => setLocale("en-US"),
          },
        ]}
        trigger={
          <Button
            aria-label={text("切换语言", "Switch language")}
            size="icon"
            variant="ghost"
          >
            <Languages size={16} />
          </Button>
        }
      />
    </div>
  );
}
