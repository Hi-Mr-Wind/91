import { ReactNode } from "react";

type Props = {
  title: string;
  extra?: ReactNode;
};

export function SectionHeader({ title, extra }: Props) {
  return (
    <div className="section-header">
      <span className="section-header__title">{title}</span>
      {extra && <span className="section-header__extra">{extra}</span>}
    </div>
  );
}
