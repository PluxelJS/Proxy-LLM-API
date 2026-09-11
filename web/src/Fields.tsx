import type { ReactNode } from "react";
import { Checkbox } from "@heroui/react/checkbox";
import { Description } from "@heroui/react/description";
import { Input, type InputProps } from "@heroui/react/input";
import { Label } from "@heroui/react/label";
import { ListBox } from "@heroui/react/list-box";
import { Select } from "@heroui/react/select";
import { TextArea, type TextAreaProps } from "@heroui/react/textarea";
import { TextField } from "@heroui/react/textfield";

type FieldProps = {
  label: ReactNode;
  description?: ReactNode;
};

export function TextInputField({
  label,
  description,
  ...props
}: FieldProps & Omit<InputProps, "variant">) {
  return (
    <TextField fullWidth variant="secondary">
      <Label>{label}</Label>
      <Input {...props} />
      {description && <Description>{description}</Description>}
    </TextField>
  );
}

export function TextAreaField({
  label,
  description,
  ...props
}: FieldProps & Omit<TextAreaProps, "variant">) {
  return (
    <TextField fullWidth variant="secondary">
      <Label>{label}</Label>
      <TextArea {...props} />
      {description && <Description>{description}</Description>}
    </TextField>
  );
}

export function BooleanField({
  label,
  description,
  isSelected,
  onChange,
}: FieldProps & {
  isSelected: boolean;
  onChange: (selected: boolean) => void;
}) {
  return (
    <Checkbox isSelected={isSelected} onChange={onChange}>
      <Checkbox.Content>
        <Checkbox.Control>
          <Checkbox.Indicator />
        </Checkbox.Control>
        <Label>{label}</Label>
      </Checkbox.Content>
      {description && <Description>{description}</Description>}
    </Checkbox>
  );
}

export type SelectOption = { id: string; label: string };

export function SelectField({
  label,
  value,
  options,
  onChange,
}: {
  label: ReactNode;
  value: string;
  options: SelectOption[];
  onChange: (value: string) => void;
}) {
  return (
    <Select
      fullWidth
      variant="secondary"
      selectedKey={value}
      onSelectionChange={(key) => key != null && onChange(String(key))}
    >
      <Label>{label}</Label>
      <Select.Trigger>
        <Select.Value />
        <Select.Indicator />
      </Select.Trigger>
      <Select.Popover>
        <ListBox items={options}>
          {(option) => (
            <ListBox.Item id={option.id} textValue={option.label}>
              {option.label}
              <ListBox.ItemIndicator />
            </ListBox.Item>
          )}
        </ListBox>
      </Select.Popover>
    </Select>
  );
}
