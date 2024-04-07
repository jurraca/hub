import React, { ReactElement } from "react";
import { Link } from "react-router-dom";
import { Button } from "src/components/ui/button";

interface Props {
  icon: ReactElement;
  title: string;
  description: string;
  buttonText: string;
  buttonLink: string;
}

const EmptyState: React.FC<Props> = ({
  icon,
  title: message,
  description: subMessage,
  buttonText,
  buttonLink,
}) => {
  return (
    <div className="flex flex-1 items-center justify-center rounded-lg border border-dashed shadow-sm">
      <div className="flex flex-col items-center gap-1 text-center">
        {React.cloneElement(icon, {
          className: "w-10 h-10 text-muted-foreground",
        })}
        <h3 className="mt-4 text-lg font-semibold">{message}</h3>
        <p className="text-sm text-muted-foreground">{subMessage}</p>
        <Link to={buttonLink}>
          <Button className="mt-4">{buttonText}</Button>
        </Link>
      </div>
    </div>
  );
};

export default EmptyState;