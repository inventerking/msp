import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import en from "../locales/en.json";

// All text lives in catalogs from the start, since the product is sold worldwide.
void i18n.use(initReactI18next).init({
  lng: "en",
  fallbackLng: "en",
  resources: { en: { translation: en } },
  interpolation: { escapeValue: false }, // React escapes values already.
});

export default i18n;
