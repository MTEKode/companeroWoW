local ADDON_NAME, ns = ...

local frame = CreateFrame("Frame")
frame:RegisterEvent("ADDON_LOADED")
frame:SetScript("OnEvent", function(self, event, name)
    if event == "ADDON_LOADED" and name == ADDON_NAME then
        CompaneroWoWDB = CompaneroWoWDB or {}
        print("|cff00ff00" .. ADDON_NAME .. "|r cargado.")
        self:UnregisterEvent("ADDON_LOADED")
    end
end)
